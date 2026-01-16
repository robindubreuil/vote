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
		AllowedOrigins: []string{"*"},
		PingInterval:   time.Second,
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
		AllowedOrigins: []string{"*"},
		PingInterval:   time.Second,
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
		AllowedOrigins: []string{"*"},
		PingInterval:   time.Second,
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
