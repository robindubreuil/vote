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
	cfg      *config.Config
	baseURL  string
	wsURL    string
	shutdown context.CancelFunc
}

// NewTestServer creates and starts a test server on a random port.
// The returned server should be closed with Close() when done.
func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	// D17: bind the listener up front and serve it directly. The prior
	// getFreePort+ListenAndServe path had a TOCTOU window: another
	// process could grab the port between the probe close and the
	// server's rebind, making the test flaky on busy CI machines.
	// Binding once on ":0" and reading l.Addr() eliminates the race;
	// the same listener is then handed to srv.Serve, so the port the
	// test advertises is exactly the port the server accepts on.
	l, port := newBoundListener(t)

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
	h.Run()

	// Create server
	srv := server.NewServer(cfg, h)

	// Start server in background, serving the pre-bound listener so the
	// port in cfg.Port is the port actually accepting connections.
	_, cancel := context.WithCancel(context.Background())

	go func() {
		if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
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
//
// R2: the order mirrors main.go's gracefulShutdown — srv.Shutdown BEFORE
// h.Shutdown. Closing the HTTP listener first stops new WS dials from
// racing the Hub's wg.Wait; the drain guard in handleWebSocket catches
// any in-flight request that slipped past the listener before close.
func (ts *TestServer) Close(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), ts.cfg.ShutdownTimeout)
	defer cancel()

	if err := ts.srv.Shutdown(ctx); err != nil {
		t.Logf("Server shutdown error: %v", err)
	}
	ts.hub.Shutdown()
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

// newBoundListener opens a listener on a kernel-assigned port and
// returns both the listener (already bound) and the port string. D17:
// closing the listener and rebinding the same port (the old
// getFreePort+ListenAndServe pattern) left a TOCTOU window; callers
// that need a free port for their own Server should use this and pass
// the listener to srv.Serve rather than reading the port and calling
// srv.Run.
func newBoundListener(t *testing.T) (net.Listener, string) {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to bind test listener: %v", err)
	}
	return l, fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
}
