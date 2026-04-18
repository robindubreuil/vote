// Package integration provides end-to-end WebSocket integration tests.
// These tests start a real HTTP/WebSocket server and connect actual clients
// using gorilla/websocket to test the full protocol flow.
package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/server"
)

// TestServer wraps a real server instance for integration testing.
type TestServer struct {
	hub      *hub.Hub
	srv      *server.Server
	httpSrv  *http.Server
	cfg      *config.Config
	baseURL  string
	wsURL    string
	shutdown context.CancelFunc
}

// NewTestServer creates and starts a test server on a random port.
// The returned server should be closed with Close() when done.
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	// Get a free port
	port := getFreePort(t)

	// Create test config
	cfg := &config.Config{
		Port:            port,
		SessionTimeout:  5 * time.Minute,
		CleanupInterval: time.Minute,
		PingInterval:    30 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 5 * time.Second,
		AllowedOrigins:  []string{"*"},
		ValidColors: []string{
			"rouge", "vert", "bleu", "jaune",
			"orange", "violet", "rose", "gris",
		},
	}

	// Set gin to release mode for tests
	gin.SetMode(gin.TestMode)

	// Silence slog output during tests
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	slog.SetDefault(logger)

	// Create and start hub
	h := hub.NewHub(cfg)
	go h.Run()

	// Create server
	srv := server.NewServer(cfg, h)

	// Start server in background
	_, cancel := context.WithCancel(context.Background())

	go func() {
		if err := srv.Run(); err != nil && err != http.ErrServerClosed {
			t.Logf("Server run error: %v", err)
		}
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%s", port)
	wsURL := fmt.Sprintf("ws://localhost:%s/ws", port)

	return &TestServer{
		hub:      h,
		srv:      srv,
		cfg:      cfg,
		baseURL:  baseURL,
		wsURL:    wsURL,
		shutdown: cancel,
	}
}

// Close gracefully shuts down the test server.
func (ts *TestServer) Close(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), ts.cfg.ShutdownTimeout)
	defer cancel()

	ts.hub.Shutdown()
	if err := ts.srv.Shutdown(ctx); err != nil {
		t.Logf("Server shutdown error: %v", err)
	}
	ts.shutdown()
}

// Hub returns the test server's hub for direct inspection.
func (ts *TestServer) Hub() *hub.Hub {
	return ts.hub
}

// WebSocketURL returns the WebSocket URL for clients to connect.
func (ts *TestServer) WebSocketURL() string {
	return ts.wsURL
}

// BaseURL returns the base HTTP URL.
func (ts *TestServer) BaseURL() string {
	return ts.baseURL
}

// getFreePort returns an available port on localhost.
func getFreePort(t *testing.T) string {
	t.Helper()

	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to resolve tcp address: %v", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to listen on tcp: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("%d", port)
}

// waitFor with a timeout, useful for synchronization in tests.
func waitFor[T any](condition func() (T, bool), timeout time.Duration) (T, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if result, ok := condition(); ok {
			return result, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	var zero T
	return zero, false
}

// requireWithTimeout waits for a condition or fails the test.
func requireWithTimeout[T any](t *testing.T, condition func() (T, bool), msg string, timeout time.Duration) T {
	t.Helper()

	if result, ok := waitFor(condition, timeout); ok {
		return result
	}
	t.Fatalf("Timeout waiting for: %s", msg)
	var zero T
	return zero
}
