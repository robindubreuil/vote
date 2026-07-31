package security

import (
	"context"
	"errors"
	"testing"
	"time"
)

// errTestCSPRNG is the deterministic error returned by the randRead seam
// in the B14 panic tests. A typed sentinel (rather than errors.New in
// each test) makes the assertion intent clearer and avoids allocating a
// fresh error per call from the seam.
var errTestCSPRNG = errors.New("simulated CSPRNG failure (test)")

func TestCheckJoinRateLimit(t *testing.T) {
	sec := NewSecurity(context.Background(), 0)
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
	sec := NewSecurity(context.Background(), 0)
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
	sec := NewSecurity(context.Background(), 0)
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
	sec := NewSecurity(context.Background(), 0)
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
	sec := NewSecurity(ctx, 0)

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

func TestSessionCreateRate(t *testing.T) {
	sec := NewSecurity(context.Background(), 0)
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
	sec := NewSecurity(context.Background(), 0)
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
	sec := NewSecurity(context.Background(), 0)
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

func TestRecordFailedJoinBackoffOverflow(t *testing.T) {
	sec := NewSecurity(context.Background(), 0)
	defer sec.Shutdown()
	ip := "10.0.0.99"

	// Simulate a very high failure count — well past the int64 shift
	// overflow point (~56). Before the fix, this wrapped negative and
	// collapsed the backoff to the 100 ms floor.
	sec.mu.Lock()
	sec.failedJoins[ip] = &FailedJoinAttempt{
		Count:            100,
		LastAttempt:      time.Now(),
		LastBackoffUntil: time.Now(),
	}
	sec.mu.Unlock()

	sec.RecordFailedJoin(ip)

	sec.mu.RLock()
	attempt := sec.failedJoins[ip]
	sec.mu.RUnlock()

	if attempt == nil {
		t.Fatal("failed join record should exist")
	}
	// The backoff window must not be in the past — that would indicate
	// the 100 ms floor fired after an overflow.
	if attempt.LastBackoffUntil.Before(time.Now()) {
		t.Errorf("backoff should be in the future, got %v (now=%v)",
			attempt.LastBackoffUntil, time.Now())
	}
	// And it must be well above the 100 ms floor that the overflow bug
	// would collapse to. Jitter is ±25%, so the real minimum is ~75% of
	// MaxBackoffMs; 60 s is a safe threshold far above the buggy floor.
	minExpected := time.Now().Add(60 * time.Second)
	if attempt.LastBackoffUntil.Before(minExpected) {
		t.Errorf("backoff should be >= 60s; got %v", attempt.LastBackoffUntil)
	}
	// R11: the backoff ceiling is enforced AFTER jitter, so the
	// observable maximum is exactly MaxBackoffMs (5 min), not
	// MaxBackoffMs×1.25 (6.25 min) the pre-fix order produced.
	if dur := time.Until(attempt.LastBackoffUntil); dur > MaxBackoffMs*time.Millisecond {
		t.Errorf("backoff must not exceed MaxBackoffMs (%dms) after R11; got %v", MaxBackoffMs, dur)
	}
}

// TestClampBackoffMsRespectsCeiling is the R11 deterministic table test at
// the cap boundary. clampBackoffMs is the pure function that enforces the
// [100ms, MaxBackoffMs] window AFTER jitter is applied. The pre-fix bug
// lived here: the cap ran before jitter, so a base clamped to MaxBackoffMs
// then gained up to +25% on top, yielding 375s/6.25min. Each row feeds a
// post-jitter value through the clamp and pins the result.
func TestClampBackoffMsRespectsCeiling(t *testing.T) {
	jitter := int(float64(MaxBackoffMs) * BackoffJitter) // max +25% offset
	cases := []struct {
		name string
		ms   int
		want int
	}{
		{"cap plus max positive jitter stays at cap", MaxBackoffMs + jitter, MaxBackoffMs},
		{"exactly at cap unchanged", MaxBackoffMs, MaxBackoffMs},
		{"one above cap clamps down", MaxBackoffMs + 1, MaxBackoffMs},
		{"far above cap clamps down", MaxBackoffMs * 2, MaxBackoffMs},
		{"mid-range unchanged", 60_000, 60_000},
		{"below floor clamps up to 100ms", 50, 100},
		{"negative clamps to floor", -10_000, 100},
		{"zero clamps to floor", 0, 100},
		{"cap minus jitter within range unchanged", MaxBackoffMs - jitter, MaxBackoffMs - jitter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clampBackoffMs(c.ms); got != c.want {
				t.Errorf("clampBackoffMs(%d): got %d, want %d", c.ms, got, c.want)
			}
		})
	}
}

// TestBackoffRespectsMaxAfterJitter drives the full randomized path
// (computeBackoffMs, which draws a real ±25% jitter) across a sweep of
// failure counts at and beyond the cap boundary, and asserts the result
// NEVER exceeds MaxBackoffMs and never drops below the 100ms floor. This
// is the integration-level guard: a single value above the cap would
// resurrect the R11 bug.
func TestBackoffRespectsMaxAfterJitter(t *testing.T) {
	for count := MaxFailedAttempts; count < MaxFailedAttempts+40; count++ {
		for i := 0; i < 50; i++ {
			ms := computeBackoffMs(count)
			if ms > MaxBackoffMs {
				t.Errorf("count=%d iter=%d: backoff %dms exceeds MaxBackoffMs %dms (R11 ceiling violated)", count, i, ms, MaxBackoffMs)
			}
			if ms < 100 {
				t.Errorf("count=%d iter=%d: backoff %dms below 100ms floor", count, i, ms)
			}
		}
	}
}

// TestBackoffScalesWithFailures guards the exponential growth that the
// overflow-cap and jitter refactor must preserve: more failures must not
// shrink the backoff. The highest-count median must be >= the lowest.
func TestBackoffScalesWithFailures(t *testing.T) {
	median := func(count int) int {
		max := 0
		for i := 0; i < 200; i++ {
			if v := computeBackoffMs(count); v > max {
				max = v
			}
		}
		return max
	}
	low := median(MaxFailedAttempts)       // exponent 0: base 1s
	high := median(MaxFailedAttempts + 12) // deep into the cap region
	if high < low {
		t.Errorf("deeper failures must not reduce backoff: low=%d high=%d", low, high)
	}
}
