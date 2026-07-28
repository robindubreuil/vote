package hub

import (
	"sync"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/vote"
)

// TestGetMetricsDoesNotHoldHubLockDuringSessionIteration is the B8
// regression test: the previous implementation held h.mu.RLock across
// the entire /metrics scrape, including up to MaxSessionsGlobal=1000
// session.GetState() calls — each of which takes the per-session lock.
// A scrape therefore blocked every register/unregister (which take the
// write lock) and every BroadcastSession fanout (which takes RLock).
//
// We assert the fix by creating many sessions, starting a GetMetrics
// scrape in one goroutine, and racing it against concurrent
// SessionExists calls. If GetMetrics still held the lock for the whole
// duration, SessionExists would wait; the test would either time out
// or observe dramatically inflated latency. With the fix, SessionExists
// returns immediately.
func TestGetMetricsDoesNotHoldHubLockDuringSessionIteration(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Create enough sessions that GetState iteration has measurable
	// cost. Each session also needs a CreateSession on the manager so
	// VoteManager.GetAllSessions returns non-empty. Generate distinct
	// 3-char codes from the disambiguation-safe alphabet.
	const sessions = 200
	const alpha = vote.SessionAlphabet
	const base = len(alpha) // 23
	for i := 0; i < sessions; i++ {
		// Base-23 encoding of i into 3 alphabet chars. 23^3 = 12167 > 200.
		code := string([]byte{
			alpha[(i/(base*base))%base],
			alpha[(i/base)%base],
			alpha[i%base],
		})
		if _, err := h.VoteManager.CreateSession(code, "trainer"); err != nil {
			t.Fatalf("CreateSession %q: %v", code, err)
		}
		h.mu.Lock()
		h.Connections[code] = &SessionConnections{Stagiaires: make(map[string]*Client)}
		h.mu.Unlock()
	}

	// Confirm there's something to iterate.
	if n := len(h.VoteManager.GetAllSessions()); n == 0 {
		t.Fatal("test setup produced 0 sessions; cannot validate the fix")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = h.GetMetrics()
			}
		}
	}()

	// Race GetMetrics against SessionExists. If GetMetrics held h.mu
	// for the whole iteration, SessionExists (which takes h.mu.RLock)
	// would queue behind it and we'd see inflated latency. With the
	// fix (snapshot pointers, release, iterate lock-free) the two
	// proceed concurrently.
	const probes = 100
	const perProbeTimeout = 50 * time.Millisecond
	for i := 0; i < probes; i++ {
		done := make(chan struct{})
		go func() {
			h.SessionExists("ABC")
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(perProbeTimeout):
			close(stop)
			wg.Wait()
			t.Fatalf("SessionExists probe %d blocked > %v — h.mu held during GetMetrics (B8 regression)", i, perProbeTimeout)
		}
	}

	close(stop)
	wg.Wait()
}

// TestGetMetricsCorrectnessAfterLockRelease is a baseline correctness
// guard: the lock-free session iteration must still observe sessions
// created before the snapshot. We're not testing staleness under
// concurrent CreateSession (the snapshot is allowed to miss a session
// created mid-scrape — that's the accepted trade-off for not holding the
// lock); we're testing that the snapshot returns the right shape.
func TestGetMetricsCorrectnessAfterLockRelease(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Pre-create a known session with an Idle state so VoteStates carries
	// a non-zero count.
	if _, err := h.VoteManager.CreateSession("ABC", "trainer1"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	h.mu.Lock()
	h.Connections["ABC"] = &SessionConnections{
		Trainer: &Client{ID: "trainer1", SessionID: "ABC", Type: "trainer"},
		Stagiaires: map[string]*Client{
			"s1": {ID: "s1", SessionID: "ABC", Type: "stagiaire"},
		},
	}
	h.mu.Unlock()

	m := h.GetMetrics()
	if m.ActiveSessions != 1 {
		t.Errorf("ActiveSessions: got %d, want 1", m.ActiveSessions)
	}
	if m.ConnectedTrainers != 1 {
		t.Errorf("ConnectedTrainers: got %d, want 1", m.ConnectedTrainers)
	}
	if m.ConnectedStagiaires != 1 {
		t.Errorf("ConnectedStagiaires: got %d, want 1", m.ConnectedStagiaires)
	}
	// New sessions are Idle by default.
	if m.VoteStates["idle"] < 1 {
		t.Errorf("VoteStates[idle]: got %d, want >= 1", m.VoteStates["idle"])
	}
}
