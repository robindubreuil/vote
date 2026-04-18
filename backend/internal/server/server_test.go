package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

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
	go h.Run()
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
	go h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
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
	go h.Run()
	defer h.Shutdown()

	srv := NewServer(cfg, h)
	ts := httptest.NewServer(srv.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	headers := http.Header{}
	headers.Set("X-Forwarded-For", "10.0.0.1")

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
