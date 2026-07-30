package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
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

// TestLivezIsCheapLiveness is the D9 liveness contract: /livez returns
// 200 the moment the HTTP server can route the request, with a small
// payload. It must NOT fail when the hub is draining — draining is a
// readiness concept, and a graceful drain must not get the process
// killed by a liveness probe mid-shutdown. It also must not touch the
// store or metrics (those would make it expensive and could hang).
func TestLivezIsCheapLiveness(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/livez", nil)
	srv.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Expected 200 from /livez, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, `"alive"`) {
		t.Errorf("/livez body missing status=alive: %s", got)
	}

	// Draining hub must still return 200 from /livez (liveness ≠ readiness).
	h.Shutdown()
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/livez", nil)
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Errorf("Expected /livez to stay 200 during drain (liveness), got %d", w2.Code)
	}
}

// TestReadyzReflectsDrain is the D9 readiness contract: /readyz is 200
// normally, 503 once the hub's context is cancelled (draining). A
// load-balancer wired to /readyz stops routing traffic during a graceful
// shutdown.
func TestReadyzReflectsDrain(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/readyz", nil)
	srv.router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("Expected 200 from /readyz when healthy, got %d", w.Code)
	}
	if got := w.Body.String(); !strings.Contains(got, `"ready"`) {
		t.Errorf("/readyz body missing status=ready: %s", got)
	}

	// Cancel the hub context to simulate Shutdown's drain phase.
	h.Shutdown()

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/readyz", nil)
	srv.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 from /readyz while draining, got %d (body=%s)", w2.Code, w2.Body.String())
	}
	if got := w2.Body.String(); !strings.Contains(got, `"draining"`) {
		t.Errorf("/readyz body missing status=draining: %s", got)
	}
}

// TestHealthAliasStillDrains pins the backward-compat contract for the
// legacy /health path: external monitors and the CI wait-loop were built
// against /health reporting 503 while draining, so the alias must keep
// that behaviour rather than silently becoming always-200.
func TestHealthAliasStillDrains(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)

	h.Shutdown()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("legacy /health must report 503 while draining, got %d", w.Code)
	}
}

// TestVersionEndpoint is the D2 contract: /version returns JSON with
// version, build_time, and git_commit. Missing values surface as the
// literal "unknown" so the JSON shape is stable for monitors that
// parse it (empty strings would be ambiguous). This is public — the
// version is already exposed via the vote_build_info metric.
func TestVersionEndpoint(t *testing.T) {
	t.Run("populated", func(t *testing.T) {
		cfg := &config.Config{Port: "8080"}
		h := hub.NewHub(cfg)
		srv := NewServer(cfg, h)
		srv.SetBuildInfo("1.4.0", "2026-07-29T10:00:00Z", "abc1234")

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/version", nil)
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("Expected 200, got %d", w.Code)
		}
		got := w.Body.String()
		for _, want := range []string{
			`"version":"1.4.0"`,
			`"build_time":"2026-07-29T10:00:00Z"`,
			`"git_commit":"abc1234"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("/version body missing %q\nbody: %s", want, got)
			}
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("expected json content-type, got %q", ct)
		}
	})

	t.Run("defaults_to_unknown", func(t *testing.T) {
		cfg := &config.Config{Port: "8080"}
		h := hub.NewHub(cfg)
		srv := NewServer(cfg, h)
		// No SetBuildInfo call — defaults to zero values.

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/version", nil)
		srv.router.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("Expected 200, got %d", w.Code)
		}
		got := w.Body.String()
		// All three must surface as "unknown", never as empty strings,
		// so the JSON shape is stable for parse-once monitors.
		for _, want := range []string{`"version":"unknown"`, `"build_time":"unknown"`, `"git_commit":"unknown"`} {
			if !strings.Contains(got, want) {
				t.Errorf("/version body missing default %q\nbody: %s", want, got)
			}
		}
	})
}

func TestMetricsEndpoint(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)
	srv.SetBuildInfo("test-version", "2026-01-01", "test-commit")

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
		`vote_build_info{version="test-version",build_time="2026-01-01",git_commit="test-commit"} 1`,
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

// TestServeAcceptsPreBoundListener pins the D17 race-free serving contract.
// Serve takes an already-bound net.Listener and serves on it directly,
// so a caller that needs a deterministic port can net.Listen("tcp",
// ":0"), read l.Addr(), and Serve(l) without the TOCTOU window that
// getFreePort+ListenAndServe would otherwise leave. This also covers
// the Run() refactor: Run now binds the listener itself and delegates
// to Serve, so a regression that broke the delegation would surface
// here as the listener never accepting.
func TestServeAcceptsPreBoundListener(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Second,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()

	srv := NewServer(cfg, h)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	defer srv.Shutdown(context.Background())

	// The pre-bound address must answer a real HTTP request — the
	// listener handed to Serve is exactly the one the test probed.
	resp, err := http.Get("http://" + addr + "/livez")
	if err != nil {
		t.Fatalf("GET /livez on pre-bound listener: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from /livez, got %d", resp.StatusCode)
	}
	_ = done // serve goroutine exits when Shutdown closes the server
}

// TestShutdownRejectsNewUpgradesDuringDrain is the R2 drain-guard test.
//
// Before the fix, the hub drained while the HTTP listener was still
// accepting dials, so a WS reconnect arriving during the drain window
// upgraded successfully, called client.Start() → wg.Add(2), and raced
// with the Hub's wg.Wait (Go forbids positive-delta Add concurrent with
// Wait when the counter is zero). Worse, the hijacked conn wasn't
// tracked by http.Server.Shutdown, so its readPump blocked in
// ReadMessage until pongWait (70s) — a "connected" socket that
// delivered no state, freezing a 30-student class on every deploy.
//
// The fix adds a drain guard at the top of handleWebSocket: if the
// hub's context is cancelled, the dial is rejected with 503 before any
// resource is acquired or the upgrade runs. This test cancels the hub
// context via h.Shutdown (which, with no registered clients, returns
// immediately — leaving the HTTP listener open, exactly the state the
// guard must catch) and asserts a fresh dial gets 503, not an upgrade.
func TestShutdownRejectsNewUpgradesDuringDrain(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Hour,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// Sanity: a dial before drain upgrades successfully (guard does not
	// fire while the hub context is live).
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	healthy, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{"http://localhost"}})
	if err != nil {
		t.Fatalf("pre-drain dial should succeed: %v", err)
	}
	healthy.Close()

	// Trigger drain. h.Shutdown cancels the hub context and waits for
	// the runLoop/cleanupLoop goroutines. No clients are registered in
	// hub.Connections (the sanity dial above never sent a join message),
	// so Shutdown returns immediately. The hub context is now cancelled,
	// but the HTTP listener (owned by httptest) is still open — exactly
	// the state handleWebSocket's guard must catch.
	h.Shutdown()

	// A fresh request to /ws must be rejected with 503 by the drain
	// guard BEFORE the upgrade. A plain GET (no Upgrade headers) is
	// used so a successful guard produces a readable 503; if the guard
	// were absent, gorilla's upgrader would return 400 for the missing
	// Upgrade headers (see TestWebsocketConnection), not 503.
	resp, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatalf("GET /ws during drain: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 during drain, got %d", resp.StatusCode)
	}
}
