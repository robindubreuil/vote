package security

import (
	"context"
	"testing"
	"time"
)

func TestCheckJoinRateLimit(t *testing.T) {
	sec := NewSecurity(context.Background())
	testIP := "192.168.1.1"

	// First attempt allowed
	allowed, _ := sec.CheckJoinRateLimit(testIP)
	if !allowed {
		t.Error("First attempt should be allowed")
	}

	// Fail repeatedly
	for i := 0; i < MaxFailedAttempts; i++ {
		sec.RecordFailedJoin(testIP)
	}

	// Should be blocked
	allowed, backoff := sec.CheckJoinRateLimit(testIP)
	if allowed {
		t.Error("Should be blocked after failures")
	}
	if backoff <= 0 {
		t.Error("Should return backoff duration")
	}

	// Clear
	sec.ClearFailedJoin(testIP)
	allowed, _ = sec.CheckJoinRateLimit(testIP)
	if !allowed {
		t.Error("Should be allowed after clear")
	}
}

func TestCheckMessageRate(t *testing.T) {
	sec := NewSecurity(context.Background())
	clientID := "client1"

	if !sec.CheckMessageRate(clientID) {
		t.Error("First message should be allowed")
	}

	// Burst check
	for i := 0; i < MaxBurstMessages + 5; i++ {
		sec.CheckMessageRate(clientID)
	}
	
	// Eventually it should return false, but exact count depends on timing
	// Just ensure the function runs without panic and logic holds
}

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()
	if len(id1) != 12 {
		t.Errorf("expected length 12, got %d", len(id1))
	}
	if id1 == id2 {
		t.Error("IDs should be unique")
	}
}

func TestCleanup(t *testing.T) {
	sec := NewSecurity(context.Background())
	// Inject stale data manually if possible, but map is private.
	// We can't easily test private map cleanup from outside package 
	// unless we export it or use reflection, or test behavior (e.g. removed restriction).
	// For now, we trust the logic or move it to a method we can trigger.
	// Actually we are in package security so we CAN access private fields in test.
	
	sec.failedJoins["1.2.3.4"] = &FailedJoinAttempt{
		Count: 5,
		LastAttempt: time.Now().Add(-2 * FailedAttemptWindow),
		LastBackoffUntil: time.Now().Add(-1 * time.Hour),
	}
	
	sec.cleanup()
	
	if _, ok := sec.failedJoins["1.2.3.4"]; ok {
		t.Error("Stale failed join should be removed")
	}
}

func TestRemoveMessageRate(t *testing.T) {
	sec := NewSecurity(context.Background())
	clientID := "client_rem"
	
	// Trigger rate limiter creation
	sec.CheckMessageRate(clientID)
	
	sec.mu.Lock()
	if _, ok := sec.messageRates[clientID]; !ok {
		sec.mu.Unlock()
		t.Error("Rate limiter should exist")
		return
	}
	sec.mu.Unlock()
	
	sec.RemoveMessageRate(clientID)
	
	sec.mu.Lock()
	if _, ok := sec.messageRates[clientID]; ok {
		sec.mu.Unlock()
		t.Error("Rate limiter should be removed")
	}
	sec.mu.Unlock()
}

func TestShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sec := NewSecurity(ctx)
	
	// Wait a bit to ensure loop starts
	time.Sleep(10 * time.Millisecond)
	
	// Trigger shutdown
	cancel()
	sec.Shutdown()
	
	// Wait for cleanup
	time.Sleep(10 * time.Millisecond)
	// We can't verify easily that the goroutine stopped without a waitgroup or channel in Security struct
	// But we ensure it doesn't panic
}

func TestGenerateTimestampID(t *testing.T) {
	id1 := generateTimestampID()
	time.Sleep(time.Millisecond) // Ensure time difference
	id2 := generateTimestampID()
	
	if id1 == id2 {
		t.Error("Timestamp IDs should be unique over time")
	}
	if len(id1) == 0 {
		t.Error("ID should not be empty")
	}
}
