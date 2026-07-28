package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

// newTestServerWithMiddleware builds a real server so the test exercises
// the production middleware wiring (Recovery → RequestID → AccessLog)
// rather than a hand-rolled gin.Engine.
func newTestServerWithMiddleware(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		CleanupInterval: time.Hour, // avoid zero-interval ticker panic
	}
	h := hub.NewHub(cfg)
	h.Run()
	t.Cleanup(h.Shutdown)
	return NewServer(cfg, h)
}

// TestRequestIDMiddlewareSetsHeader is the B7 regression: every response
// must carry an X-Request-ID so an operator can correlate a browser
// network entry with a server log line.
func TestRequestIDMiddlewareSetsHeader(t *testing.T) {
	srv := newTestServerWithMiddleware(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	srv.router.ServeHTTP(w, req)

	if id := w.Header().Get(requestIDHeader); id == "" {
		t.Errorf("response missing %s header", requestIDHeader)
	}
}

// TestRequestIDMiddlewarePreservesInboundID pins the upstream-proxy
// contract: if the inbound request already carries an X-Request-ID,
// it is preserved on the response (and surfaced to downstream handlers)
// rather than being overwritten with a new one. An operator who set up
// a proxy that mints its own IDs can still correlate end-to-end.
func TestRequestIDMiddlewarePreservesInboundID(t *testing.T) {
	srv := newTestServerWithMiddleware(t)

	const inbound = "upstream-trace-abc-123"
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	req.Header.Set(requestIDHeader, inbound)
	srv.router.ServeHTTP(w, req)

	if got := w.Header().Get(requestIDHeader); got != inbound {
		t.Errorf("inbound ID not preserved: got %q, want %q", got, inbound)
	}
}

// TestRequestIDMiddlewareRegeneratesOverlongInbound guards against a
// log-injection / log-flooding vector: an attacker who controls the
// inbound X-Request-ID could ship a multi-kilobyte value that bloats
// every subsequent log line. The middleware regenerates when the
// inbound value is longer than a small cap.
func TestRequestIDMiddlewareRegeneratesOverlongInbound(t *testing.T) {
	srv := newTestServerWithMiddleware(t)

	overlong := strings.Repeat("a", 200)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	req.Header.Set(requestIDHeader, overlong)
	srv.router.ServeHTTP(w, req)

	got := w.Header().Get(requestIDHeader)
	if got == overlong {
		t.Error("overlong inbound ID must be regenerated, not echoed")
	}
	if len(got) > 64 {
		t.Errorf("regenerated ID too long: %d bytes", len(got))
	}
}

// TestRequestIDAvailableToHandlers confirms the ID lands on the gin
// context (the contract RequestIDFromContext exposes) so handlers and
// downstream log calls can enrich their own lines with it.
func TestRequestIDAvailableToHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestIDMiddleware())
	var seen string
	r.GET("/probe", func(c *gin.Context) {
		seen = RequestIDFromContext(c)
		c.Status(204)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/probe", nil)
	r.ServeHTTP(w, req)

	if seen == "" {
		t.Error("handler did not see the request ID on the context")
	}
	if got := w.Header().Get(requestIDHeader); got != seen {
		t.Errorf("context ID and header ID disagree: ctx=%q header=%q", seen, got)
	}
}

// TestRequestIDFromContextEmptyWhenUnset covers the absent-context
// branch (e.g. code that constructs a gin.Context manually without the
// middleware): the helper must return "", not panic.
func TestRequestIDFromContextEmptyWhenUnset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := RequestIDFromContext(c); got != "" {
		t.Errorf("expected empty when no middleware ran, got %q", got)
	}
}

// TestAccessLogMiddlewareEmitsLine confirms one structured line is
// emitted per request and that it carries the correlation fields an
// operator needs: method, path, status, latency, ip, request_id.
func TestAccessLogMiddlewareEmitsLine(t *testing.T) {
	buf, restore := captureSlogBufAt(t, slog.LevelDebug)
	defer restore()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestIDMiddleware())
	r.Use(accessLogMiddleware())
	r.GET("/p", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/p", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	r.ServeHTTP(w, req)

	out := buf.String()
	for _, want := range []string{
		"http request",
		"method=GET",
		"path=/p",
		"status=200",
		"latency=",
		"ip=1.2.3.4",
		"request_id=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("access log missing %q\noutput:\n%s", want, out)
		}
	}
}

// TestAccessLogMiddlewareLevelByStatus pins the severity routing: a 5xx
// is logged at ERROR, a 4xx at WARN, and a 2xx at INFO. We capture at
// DEBUG and assert the level= token, which the text handler emits per
// record.
func TestAccessLogMiddlewareLevelByStatus(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantTerm string
	}{
		{"2xx_info", 200, "level=INFO"},
		{"4xx_warn", 404, "level=WARN"},
		{"5xx_error", 500, "level=ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, restore := captureSlogBufAt(t, slog.LevelDebug)
			defer restore()

			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(requestIDMiddleware())
			r.Use(accessLogMiddleware())
			r.GET("/p", func(c *gin.Context) { c.Status(tc.status) })

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/p", nil)
			r.ServeHTTP(w, req)

			if !strings.Contains(buf.String(), tc.wantTerm) {
				t.Errorf("status %d: expected %q in output, got:\n%s", tc.status, tc.wantTerm, buf.String())
			}
		})
	}
}

// TestAccessLogSuppressesHealthAndMetricsAtInfo confirms the volume-
// control contract: /health and /metrics are polled at fixed cadences
// (liveness, scrape, dashboard polling) and would drown the log at
// INFO. The middleware downgrades them to DEBUG, which the default
// INFO handler drops.
func TestAccessLogSuppressesHealthAndMetricsAtInfo(t *testing.T) {
	// Capture at INFO (the production default). DEBUG lines should be
	// filtered out, so the access log for these paths is invisible.
	buf, restore := captureSlogBufAt(t, slog.LevelInfo)
	defer restore()

	srv := newTestServerWithMiddleware(t)

	for _, path := range []string{"/health", "/metrics"} {
		buf.Reset()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", path, nil)
		srv.router.ServeHTTP(w, req)

		if strings.Contains(buf.String(), "http request") {
			t.Errorf("path %q should be suppressed at INFO (polled too often); got line:\n%s", path, buf.String())
		}
	}
}

// TestRequestIDGeneratedIsLowercaseHex is a sanity guard: the generated
// ID is hex so it can be embedded in JSON, URLs, log lines, and HTML
// attributes without escaping concerns. Asserting the charset keeps the
// contract explicit for any future refactor.
func TestRequestIDGeneratedIsLowercaseHex(t *testing.T) {
	srv := newTestServerWithMiddleware(t)
	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/health", nil)
		srv.router.ServeHTTP(w, req)
		id := w.Header().Get(requestIDHeader)
		if len(id) != requestIDBytes*2 {
			t.Errorf("ID length: got %d, want %d (id=%q)", len(id), requestIDBytes*2, id)
		}
		for _, r := range id {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Fatalf("non-hex char %q in request ID %q", r, id)
			}
		}
	}
}
