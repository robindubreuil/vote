package hub

import (
	"testing"
	"time"
    "vote-backend/internal/config"
)

func TestNewHub(t *testing.T) {
    cfg := &config.Config{
        SessionTimeout: time.Hour,
        CleanupInterval: time.Hour,
    }
	h := NewHub(cfg)
	if h.Connections == nil {
		t.Error("Connections map not initialized")
	}
	if h.VoteManager == nil {
		t.Error("VoteManager not initialized")
	}
}

func TestHubSessionLifecycle(t *testing.T) {
    cfg := &config.Config{
        SessionTimeout: time.Hour,
        CleanupInterval: time.Hour,
    }
	h := NewHub(cfg)
    go h.Run()
    defer h.Shutdown()

    // Fake trainer client - use 12-char lowercase alphanumeric ID matching GenerateID format
    trainer := &Client{
        ID: "trainer1abcde",
        SessionID: "1234",
        Type: "trainer",
        Send: make(chan []byte, 10),
        Hub: h,
    }

    // Register trainer
    h.Register <- trainer

    // Wait for registration
    time.Sleep(10 * time.Millisecond)

    if !h.SessionExists("1234") {
        t.Error("Session should exist")
    }

    // Fake stagiaire - use 12-char lowercase alphanumeric ID matching GenerateID format
    stagiaire := &Client{
        ID: "s1abc1234567",
        SessionID: "1234",
        Type: "stagiaire",
        Name: "Bob",
        Send: make(chan []byte, 10),
        Hub: h,
    }

    h.Register <- stagiaire
    time.Sleep(10 * time.Millisecond)

    // Check connections
    h.mu.RLock()
    conns := h.Connections["1234"]
    h.mu.RUnlock()

    if _, ok := conns.Stagiaires["s1abc1234567"]; !ok {
        t.Error("Stagiaire should be connected")
    }

    // Unregister
    h.Unregister <- stagiaire
    time.Sleep(10 * time.Millisecond)

    h.mu.RLock()
    conns = h.Connections["1234"]
    h.mu.RUnlock()

    if _, ok := conns.Stagiaires["s1abc1234567"]; ok {
        t.Error("Stagiaire should be disconnected")
    }
}

func TestHubCleanup(t *testing.T) {
    cfg := &config.Config{
        SessionTimeout: 10 * time.Millisecond,
        CleanupInterval: 50 * time.Millisecond,
        PingInterval: time.Hour,
    }
    h := NewHub(cfg)
    
    // Create session in manager
    h.VoteManager.CreateSession("expired_session", "t1")
    
    // Create entry in Hub Connections
    h.Connections["expired_session"] = &SessionConnections{}
    
    // Start loop
    go h.cleanupLoop()
    defer h.Shutdown()
    
    // Wait for cleanup
    time.Sleep(200 * time.Millisecond)
    
    h.mu.RLock()
    _, exists := h.Connections["expired_session"]
    h.mu.RUnlock()
    
    if exists {
        t.Error("Expired session connection should have been cleaned up")
    }
}
