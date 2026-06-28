package security

import (
	"context"
	"testing"
	"time"
)

func TestCheckJoinRateLimit(t *testing.T) {
	sec := NewSecurity(context.Background())
	defer sec.Shutdown()
	testIP := "192.168.1.1"

	// First attempt allowed
	allowed := sec.CheckJoinRateLimit(testIP)
	if !allowed {
		t.Error("First attempt should be allowed")
	}

	// Fail repeatedly
	for i := 0; i < MaxFailedAttempts; i++ {
		sec.RecordFailedJoin(testIP)
	}

	// Should be blocked
	allowed = sec.CheckJoinRateLimit(testIP)
	if allowed {
		t.Error("Should be blocked after failures")
	}

	// Clear
	sec.ClearFailedJoin(testIP)
	allowed = sec.CheckJoinRateLimit(testIP)
	if !allowed {
		t.Error("Should be allowed after clear")
	}
}

func TestCheckMessageRate(t *testing.T) {
	sec := NewSecurity(context.Background())
	defer sec.Shutdown()
	clientID := "client1"

	if !sec.CheckMessageRate(clientID) {
		t.Fatal("First message should be allowed")
	}

	denied := 0
	for i := 0; i < MaxBurstMessages+10; i++ {
		if !sec.CheckMessageRate(clientID) {
			denied++
		}
	}
	if denied == 0 {
		t.Errorf("expected some messages to be denied after burst limit (%d), got 0 denials", MaxBurstMessages)
	}
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
	defer sec.Shutdown()
	// Inject stale data manually if possible, but map is private.
	// We can't easily test private map cleanup from outside package
	// unless we export it or use reflection, or test behavior (e.g. removed restriction).
	// For now, we trust the logic or move it to a method we can trigger.
	// Actually we are in package security so we CAN access private fields in test.

	sec.failedJoins["1.2.3.4"] = &FailedJoinAttempt{
		Count:            5,
		LastAttempt:      time.Now().Add(-2 * FailedAttemptWindow),
		LastBackoffUntil: time.Now().Add(-1 * time.Hour),
	}

	sec.cleanup()

	if _, ok := sec.failedJoins["1.2.3.4"]; ok {
		t.Error("Stale failed join should be removed")
	}
}

func TestRemoveMessageRate(t *testing.T) {
	sec := NewSecurity(context.Background())
	defer sec.Shutdown()
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

func TestSessionCreateRate(t *testing.T) {
	sec := NewSecurity(context.Background())
	defer sec.Shutdown()
	ip := "10.0.0.1"

	// First MaxSessionCreationsPerHour attempts must be allowed.
	for i := 0; i < MaxSessionCreationsPerHour; i++ {
		if !sec.CheckSessionCreateRate(ip) {
			t.Fatalf("attempt %d should be allowed (limit=%d)", i+1, MaxSessionCreationsPerHour)
		}
		sec.RecordSessionCreation(ip)
	}

	// Next call must be blocked.
	if sec.CheckSessionCreateRate(ip) {
		t.Error("should be blocked after the cap is reached")
	}

	// Different IP is independent — the limit is per IP, which matters for
	// the shared-NAT classroom scenario.
	otherIP := "10.0.0.2"
	if !sec.CheckSessionCreateRate(otherIP) {
		t.Error("different IP should not be affected by another IP's quota")
	}
}

func TestSessionCreateRateRollback(t *testing.T) {
	sec := NewSecurity(context.Background())
	defer sec.Shutdown()
	ip := "10.0.0.1"

	sec.RecordSessionCreation(ip)
	before := sec.CountSessionCreations(ip)
	sec.RemoveSessionCreation(ip)
	after := sec.CountSessionCreations(ip)

	if after != before-1 {
		t.Errorf("rollback should decrement count: before=%d after=%d", before, after)
	}

	// Removing on empty is a no-op.
	sec.RemoveSessionCreation(ip)
	if sec.CountSessionCreations(ip) != 0 {
		t.Error("remove on empty should be a no-op")
	}
}

func TestSessionCreateRateWindowExpiry(t *testing.T) {
	sec := NewSecurity(context.Background())
	defer sec.Shutdown()
	ip := "10.0.0.1"

	// Inject an old stamp that should be aged out on the next check.
	sec.mu.Lock()
	sec.sessionCreations[ip] = []time.Time{
		time.Now().Add(-SessionCreationWindow - time.Minute),
		time.Now().Add(-SessionCreationWindow - time.Hour),
	}
	sec.mu.Unlock()

	if !sec.CheckSessionCreateRate(ip) {
		t.Error("stale stamps should not count against the limit")
	}
	// The check itself prunes stale entries.
	if got := sec.CountSessionCreations(ip); got != 0 {
		t.Errorf("expected 0 stamps after prune, got %d", got)
	}
}
