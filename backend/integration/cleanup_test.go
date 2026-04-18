package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/server"
)

// TestSessionCleanup verifies that sessions are properly cleaned up after timeout.
func TestSessionCleanup(t *testing.T) {
	// 1. Setup server with short timeout
	cfg := &config.Config{
		Port:            getFreePort(t),
		SessionTimeout:  1 * time.Second,        // Very short timeout
		CleanupInterval: 500 * time.Millisecond, // Run cleanup frequently
		PingInterval:    30 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 5 * time.Second,
		AllowedOrigins:  []string{"*"},
		ValidColors:     []string{"rouge", "bleu"},
	}

	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	slog.SetDefault(logger)

	h := hub.NewHub(cfg)
	go h.Run()

	srv := server.NewServer(cfg, h)
	go func() {
		if err := srv.Run(); err != nil && err != http.ErrServerClosed {
			t.Logf("Server run error: %v", err)
		}
	}()
	defer func() {
		h.Shutdown()
		srv.Shutdown(context.Background())
	}()

	time.Sleep(100 * time.Millisecond) // Wait for server start
	wsURL := fmt.Sprintf("ws://localhost:%s/ws", cfg.Port)

	// 2. Run a short burst of activity
	numSessions := 5
	clients := make([]*LoadClient, 0)

	for i := 0; i < numSessions; i++ {
		// Trainer joins
		trainer, err := NewLoadClient(wsURL)
		if err != nil {
			t.Fatalf("Failed to create trainer: %v", err)
		}
		clients = append(clients, trainer)

		if err := trainer.SendMessage(TrainerJoin("").Build()); err != nil {
			t.Fatalf("Trainer join failed: %v", err)
		}
		msg, err := trainer.WaitForType("session_created", 2*time.Second)
		if err != nil {
			t.Fatalf("Session creation failed: %v", err)
		}
		sessionCode := msg["sessionCode"].(string)

		// Stagiaire joins
		stagiaire, err := NewLoadClient(wsURL)
		if err != nil {
			t.Fatalf("Failed to create stagiaire: %v", err)
		}
		clients = append(clients, stagiaire)

		if err := stagiaire.SendMessage(StagiaireJoin(sessionCode, "s1", "User1").Build()); err != nil {
			t.Fatalf("Stagiaire join failed: %v", err)
		}
		if _, err := stagiaire.WaitForType("session_joined", 2*time.Second); err != nil {
			t.Fatalf("Session join failed: %v", err)
		}
	}

	// Verify we have active sessions
	metrics := h.GetMetrics()
	if metrics.ActiveSessions != numSessions {
		t.Errorf("Expected %d active sessions, got %d", numSessions, metrics.ActiveSessions)
	}
	t.Logf("Active sessions before cleanup: %d", metrics.ActiveSessions)

	// 3. Disconnect all clients to stop activity updates
	for _, c := range clients {
		c.Close()
	}

	// 4. Wait for SessionTimeout + CleanupInterval + buffer
	// Timeout is 1s, Cleanup is 0.5s.
	// Wait 2s to be safe.
	t.Log("Waiting for cleanup...")
	time.Sleep(2500 * time.Millisecond)

	// 5. Verify Cleanup
	metrics = h.GetMetrics()
	if metrics.ActiveSessions != 0 {
		t.Errorf("Expected 0 active sessions after cleanup, got %d", metrics.ActiveSessions)
	}

	// Verify VoteManager internal state
	if len(h.VoteManager.GetAllSessions()) != 0 {
		t.Errorf("VoteManager still has %d sessions", len(h.VoteManager.GetAllSessions()))
	}

	// Verify Hub internal connection map
	if count := h.GetConnectionCount(); count != 0 {
		t.Errorf("Hub connections map size: expected 0, got %d", count)
	}
}
