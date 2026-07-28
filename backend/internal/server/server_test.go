package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

// mutexBuffer is a goroutine-safe bytes.Buffer. slog's handler writes to
// it from the HTTP-server goroutine; tests read it from the test
// goroutine. Without synchronization the race detector fires on every
// access-log assertion (B7), because the access line is emitted AFTER
// c.Next() returns — for a hijacked /ws handler the dial can complete
// before the middleware's post-Next write lands.
type mutexBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (m *mutexBuffer) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *mutexBuffer) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.String()
}

func (m *mutexBuffer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf.Reset()
}

// captureSlogBuf swaps slog.Default() for a text handler writing to a
// mutex-protected buffer at DEBUG. Tests use this to inspect middleware-
// emitted log lines (B7) without racing the HTTP-server goroutine that
// emits them. Restore via the returned func (deferred).
func captureSlogBuf(t *testing.T) (*mutexBuffer, func()) {
	return captureSlogBufAt(t, slog.LevelDebug)
}

// captureSlogBufAt is the level-configurable variant, used by tests that
// assert specific severity routing (e.g. the INFO-capture test that
// verifies /health and /metrics are suppressed at INFO).
func captureSlogBufAt(t *testing.T, level slog.Level) (*mutexBuffer, func()) {
	t.Helper()
	mb := &mutexBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(mb, &slog.HandlerOptions{Level: level})))
	return mb, func() { slog.SetDefault(prev) }
}

// satisfy io.Writer at compile time for mutexBuffer.
var _ io.Writer = (*mutexBuffer)(nil)

func TestHealthCheck(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	srv.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)
	srv.SetBuildInfo("test-version", "2026-01-01")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	srv.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	expectedMetrics := []string{
		"# HELP vote_uptime_seconds",
		"# TYPE vote_uptime_seconds gauge",
		"vote_uptime_seconds",
		"# HELP vote_sessions_active",
		"vote_sessions_active 0",
		"# HELP vote_trainers_connected",
		"vote_trainers_connected 0",
		"# HELP vote_stagiaires_connected",
		"vote_stagiaires_connected 0",
		`vote_sessions_by_state{state="idle"}`,
		`vote_sessions_by_state{state="active"}`,
		`vote_sessions_by_state{state="closed"}`,
		"# HELP go_goroutines",
		"# HELP go_mem_alloc_bytes",
		"# HELP go_gc_total",
		`vote_build_info{version="test-version",build_time="2026-01-01"} 1`,
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(body, expected) {
			t.Errorf("Metrics body missing expected string %q\nBody:\n%s", expected, body)
		}
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected text/plain content type, got %q", contentType)
	}
}

func TestSetupCORS(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins: []string{"http://example.com"},
	}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/ws", nil)
	req.Header.Set("Origin", "http://example.com")
	srv.router.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("Expected 204 for allowed origin options, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Error("Missing CORS header")
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("OPTIONS", "/ws", nil)
	req.Header.Set("Origin", "http://evil.com")
	srv.router.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("Expected 403 for denied origin, got %d", w.Code)
	}
}

func TestWebsocketConnection(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Second,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	// Should be 400 Bad Request because we didn't send Upgrade headers
	if resp.StatusCode != 400 {
		t.Errorf("Expected 400 for non-ws request to /ws, got %d", resp.StatusCode)
	}
}

func TestWebSocketSuccess(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Second,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	dialer := websocket.Dialer{}
	headers := http.Header{"Origin": []string{"http://localhost"}}
	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("Failed to connect to WS: %v", err)
	}
	defer conn.Close()

	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
	if err != nil {
		t.Errorf("Failed to write message: %v", err)
	}
}

func TestWebSocketWithProxyHeader(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Second,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	headers := http.Header{}
	headers.Set("X-Forwarded-For", "10.0.0.1")
	headers.Set("Origin", "http://localhost")

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("Failed to connect to WS: %v", err)
	}
	defer conn.Close()

	// Sending a message should work, and internally the IP should be recorded as 10.0.0.1
	// We can't verify the IP easily without inspecting internal state,
	// but this ensures the code path covering header extraction is executed.
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
	if err != nil {
		t.Errorf("Failed to write message: %v", err)
	}
}

// TestWebSocketRejectsEmptyOrigin covers S3: the CheckOrigin upgrader must
// reject a connection that carries no Origin header. Browsers always send
// Origin on cross-origin WS handshakes; absence signals a scripted client,
// which is the exact profile of a takeover/smuggling attempt.
func TestWebSocketRejectsEmptyOrigin(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Second,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// Even with a wildcard-origins config, empty Origin must be rejected —
	// the check fires before IsOriginAllowed.
	dialer := websocket.Dialer{}
	_, _, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("expected handshake failure for empty Origin, got success")
	}
}

// TestWebSocketPropagatesRequestIDToClient is the B7 correlation
// contract: the per-request ID minted at the HTTP layer (or preserved
// from an upstream proxy) is captured into Client.RequestID so every
// hub log line emitted on that client's behalf can be tied back to the
// access-log line that recorded the WS upgrade.
//
// We can't read Client.RequestID directly from the dialer side, but we
// can assert that the access log for the /ws upgrade carries the
// inbound X-Request-ID — which is the same field hub log lines will
// emit once Client.RequestID is populated. The header-on-response
// contract is verified separately for non-WS responses (health, etc.);
// gorilla/websocket's Upgrade hijacks the conn before the response is
// finalized, so we don't assert it here.
func TestWebSocketPropagatesRequestIDToClient(t *testing.T) {
	buf, restore := captureSlogBuf(t)
	defer restore()

	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Second,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// waitForLog polls the (racing) buffer until the predicate matches
	// or the deadline expires. The access-log line is written by the
	// server's HTTP-handler goroutine AFTER the dial completes, so a
	// synchronous read of buf.String() would race against that write.
	waitForLog := func(pred func(line string) bool) (string, bool) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			// Restore swaps the default logger under the package's
			// back-pointer; reads here happen via a fresh String()
			// call which observes the buffer's current length. We
			// accept the benign race on String() (it's a test) but
			// the race detector complains — so we serialize via a
			// 5ms sleep between samples.
			s := buf.String()
			if pred(s) {
				return s, true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return buf.String(), false
	}

	t.Run("inbound_id_in_access_log", func(t *testing.T) {
		buf.Reset()
		dialer := websocket.Dialer{}
		headers := http.Header{}
		headers.Set("Origin", "http://localhost")
		const inboundID = "trace-from-test-xyz"
		headers.Set("X-Request-ID", inboundID)

		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		out, ok := waitForLog(func(s string) bool {
			return strings.Contains(s, "path=/ws") && strings.Contains(s, "request_id="+inboundID)
		})
		if !ok {
			t.Errorf("access log for /ws should carry inbound request_id\noutput:\n%s", out)
		}
	})

	t.Run("generated_id_in_access_log", func(t *testing.T) {
		buf.Reset()
		dialer := websocket.Dialer{}
		headers := http.Header{"Origin": []string{"http://localhost"}}
		conn, _, err := dialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()

		out, ok := waitForLog(func(s string) bool {
			if !strings.Contains(s, "path=/ws") {
				return false
			}
			// Confirm request_id=<non-empty> is present on the /ws line.
			for _, line := range strings.Split(s, "\n") {
				if !strings.Contains(line, "path=/ws") {
					continue
				}
				idx := strings.Index(line, "request_id=")
				if idx < 0 {
					continue
				}
				rest := line[idx+len("request_id="):]
				end := strings.IndexAny(rest, " \n")
				if end == -1 {
					end = len(rest)
				}
				if len(rest[:end]) > 0 {
					return true
				}
			}
			return false
		})
		if !ok {
			t.Errorf("access log for /ws missing non-empty request_id field\noutput:\n%s", out)
		}
	})
}
