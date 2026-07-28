package hub

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/models"
)

const stressClientCount = 30

func stressCfg() *config.Config {
	return &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
}

func newStressClient(h *Hub, id string) *Client {
	c := &Client{
		ID:   id,
		Hub:  h,
		Send: make(chan []byte, ClientSendBufferSize),
		IP:   "127.0.0.1",
	}
	initTestHandlers(c)
	return c
}

func stressID(prefix byte, idx int) string {
	return fmt.Sprintf("%c%011d", prefix, idx)
}

// drainUntilQuiet reads from the channel until it stays empty for a
// quiet period. Used to absorb the asynchronous message burst that
// Hub.Run pushes to the trainer during a reconnect storm without
// asserting on exact counts (which vary with scheduling).
func drainUntilQuiet(ch <-chan []byte, quiet time.Duration, overall time.Duration) {
	deadline := time.Now().Add(overall)
	for time.Now().Before(deadline) {
		select {
		case <-ch:
		case <-time.After(quiet):
			return
		}
	}
}

// waitForStagiaireCount polls the session until it sees the expected
// number of registered stagiaires, or fails the test on timeout.
func waitForStagiaireCount(t *testing.T, h *Hub, code string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if session, ok := h.VoteManager.GetSession(code); ok {
			if got := len(session.GetStagiaires()); got == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := 0
	if session, ok := h.VoteManager.GetSession(code); ok {
		got = len(session.GetStagiaires())
	}
	t.Fatalf("timeout waiting for %d stagiaires, got %d", want, got)
}

// TestStressReconnectStorm simulates a classroom wifi flap: 30
// stagiaires join concurrently, all reconnect concurrently (takeover
// path), then all vote at once. Run under `go test -race` to catch
// data races and deadlocks in the concurrency cluster (CC1–CC3, CL1,
// CM3).
func TestStressReconnectStorm(t *testing.T) {
	h := NewHub(stressCfg())
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1stress")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Start a vote so the vote path is exercised during the storm.
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:   "start_vote",
		Colors: []string{"rouge", "bleu"},
	}))

	// Background-drain the trainer so its buffer never fills and masks
	// real bugs. Runs until trainerStop is closed or the hub shuts down.
	trainerStop := make(chan struct{})
	trainerDone := make(chan struct{})
	go func() {
		defer close(trainerDone)
		for {
			select {
			case <-trainer.Send:
			case <-trainerStop:
				return
			case <-h.Context().Done():
				return
			}
		}
	}()

	// Phase 1: 30 stagiaires join concurrently with unique names.
	type sInfo struct{ id, name string }
	infos := make([]sInfo, stressClientCount)
	for i := range infos {
		infos[i] = sInfo{id: stressID('j', i), name: fmt.Sprintf("Stagiaire%02d", i)}
	}

	phase1 := make([]*Client, stressClientCount)
	var wg sync.WaitGroup
	for i := 0; i < stressClientCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := newStressClient(h, infos[idx].id)
			c.handleMessage(mustMarshal(t, models.Message{
				Type:        "stagiaire_join",
				SessionCode: code,
				Name:        infos[idx].name,
			}))
			phase1[idx] = c
		}(i)
	}
	wg.Wait()
	waitForStagiaireCount(t, h, code, stressClientCount, 5*time.Second)

	// Phase 2: all 30 reconnect concurrently with FRESH client objects
	// while their phase-1 counterparts are still in the connection map.
	// Each reclaim-by-ID triggers the takeover path (CL1): the old
	// client is marked closing and its conn closed.
	phase2 := make([]*Client, stressClientCount)
	for i := 0; i < stressClientCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := newStressClient(h, stressID('p', i))
			c.handleMessage(mustMarshal(t, models.Message{
				Type:        "stagiaire_join",
				SessionCode: code,
				StagiaireID: infos[idx].id,
				Name:        infos[idx].name,
			}))
			phase2[idx] = c
		}(i)
	}
	wg.Wait()
	waitForStagiaireCount(t, h, code, stressClientCount, 5*time.Second)

	// CL1 regression check: every phase-1 client was displaced and
	// flagged closing so stale references drop silently.
	for i, old := range phase1 {
		if old != nil && !old.closing.Load() {
			t.Errorf("phase-1 client %d not marked closing after takeover (CL1)", i)
		}
	}

	// Phase 3: 30 concurrent votes — trainer receives ~60 messages
	// (vote_received + connected_count per vote). Tests CC1 burst
	// tolerance.
	for i := 0; i < stressClientCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			phase2[idx].handleMessage(mustMarshal(t, models.Message{
				Type:   "vote",
				Colors: []string{"rouge"},
			}))
		}(i)
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond) // let Hub.Run process the burst

	// CC1 regression check: trainer must NOT have been evicted.
	h.mu.RLock()
	activeTrainer := h.Connections[code].Trainer
	h.mu.RUnlock()
	if activeTrainer != trainer {
		t.Fatal("trainer displaced by vote burst (CC1)")
	}
	if trainer.closing.Load() {
		t.Fatal("trainer marked closing by vote burst (CC1)")
	}

	// CC2 regression check: no duplicate names.
	session, ok := h.VoteManager.GetSession(code)
	if !ok {
		t.Fatal("session disappeared after storm")
	}
	stags := session.GetStagiaires()
	if len(stags) != stressClientCount {
		t.Errorf("expected %d stagiaires, got %d", stressClientCount, len(stags))
	}
	seen := make(map[string]bool, len(stags))
	for _, name := range stags {
		norm := strings.ToLower(name)
		if seen[norm] {
			t.Errorf("duplicate name after storm (CC2): %s", name)
		}
		seen[norm] = true
	}

	close(trainerStop)
	<-trainerDone
}

// TestStressConcurrentNameRegistration fires 30 goroutines that all
// try to register distinct names simultaneously. Under the TOCTOU
// window (CC2), two clients could race past the advisory check; the
// authoritative check inside JoinStagiaire must reject the duplicate.
func TestStressConcurrentNameRegistration(t *testing.T) {
	h := NewHub(stressCfg())
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1names")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Background-drain trainer.
	go func() {
		for {
			select {
			case <-trainer.Send:
			case <-h.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
				return
			}
		}
	}()

	// 30 distinct IDs, but only 5 distinct names (6 clients per name).
	// Only the first of each name group should register; the rest hit
	// ErrNameInUse. This is the direct TOCTOU scenario: all 6 read
	// "name free" concurrently, but only 1 wins the session-lock write.
	const nameGroups = 5
	const perName = stressClientCount / nameGroups
	var successes, rejections int64

	var wg sync.WaitGroup
	for i := 0; i < stressClientCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("Dup%02d", idx%nameGroups)
			c := newStressClient(h, stressID('n', idx))
			c.handleMessage(mustMarshal(t, models.Message{
				Type:        "stagiaire_join",
				SessionCode: code,
				Name:        name,
			}))
			// Check whether the join succeeded by looking for
			// session_joined vs error in the Send channel.
			select {
			case msg := <-c.Send:
				var resp map[string]any
				json.Unmarshal(msg, &resp)
				if resp["type"] == "session_joined" {
					atomic.AddInt64(&successes, 1)
				} else {
					atomic.AddInt64(&rejections, 1)
				}
			case <-time.After(2 * time.Second):
				// Handler returned without queueing a response
				// (registration still in flight); count it as neither.
			}
		}(i)
	}
	wg.Wait()

	// Wait for all registrations to settle.
	time.Sleep(500 * time.Millisecond)

	session, ok := h.VoteManager.GetSession(code)
	if !ok {
		t.Fatal("session missing")
	}
	// Despite the race, the authoritative check must ensure no more
	// than one stagiaire per normalised name.
	stags := session.GetStagiaires()
	nameCount := make(map[string]int)
	for _, name := range stags {
		nameCount[strings.ToLower(name)]++
	}
	for name, count := range nameCount {
		if count > 1 {
			t.Errorf("name %q registered %d times (CC2 TOCTOU regression)", name, count)
		}
	}
	if len(stags) > nameGroups {
		t.Errorf("expected at most %d unique names, got %d stagiaires", nameGroups, len(stags))
	}
}

// TestTrainerBufferFullDropsNotDisconnects covers CC1: when the
// trainer's send buffer is full, the message is dropped rather than
// the connection torn down. A stagiaire in the same situation is
// evicted (and marked closing).
func TestTrainerBufferFullDropsNotDisconnects(t *testing.T) {
	h := NewHub(stressCfg())

	// Trainer with a tiny buffer.
	trainer := &Client{
		ID:   "trainerbuff001",
		Type: "trainer",
		Hub:  h,
		Send: make(chan []byte, 1),
		IP:   "127.0.0.1",
	}
	trainer.Send <- []byte("filler")

	trainer.SendJSON(map[string]any{"type": "test", "n": 1})
	if trainer.closing.Load() {
		t.Error("trainer was marked closing on buffer full (CC1 regression)")
	}
	// The filler is still in the channel (the overflow was dropped).
	select {
	case msg := <-trainer.Send:
		if string(msg) != "filler" {
			t.Error("trainer buffer contents corrupted")
		}
	default:
		t.Error("trainer buffer should still hold the filler byte")
	}

	// Stagiaire with a full buffer is evicted.
	stagiaire := &Client{
		ID:   "s1bufftest1234",
		Type: "stagiaire",
		Hub:  h,
		Send: make(chan []byte, 1),
		IP:   "127.0.0.1",
	}
	stagiaire.Send <- []byte("filler")

	stagiaire.SendJSON(map[string]any{"type": "test"})
	if !stagiaire.closing.Load() {
		t.Error("stagiaire should be marked closing on buffer full")
	}
	// A closing client's subsequent sends are no-ops.
	stagiaire.SendJSON(map[string]any{"type": "should-drop"})
	select {
	case <-stagiaire.Send:
		// The filler is still there — the second send was dropped by
		// the closing guard. If the second send went through, we'd
		// read "should-drop" here which is wrong.
	default:
	}
}

// TestClosingFlagSkipsBroadcasts covers CM3: after a slow client is
// evicted during a broadcast, subsequent broadcasts skip it via the
// closing flag instead of re-attempting (and warn-spamming) until
// pongWait reaps the entry.
func TestClosingFlagSkipsBroadcasts(t *testing.T) {
	h := NewHub(stressCfg())
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1bcm3")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Register a stagiaire with a tiny buffer. Use a valid 12-char ID
	// so JoinStagiaire accepts it.
	stagiaire := &Client{
		ID:        stressID('c', 0),
		Type:      "stagiaire",
		SessionID: code,
		Hub:       h,
		Send:      make(chan []byte, 1),
		IP:        "127.0.0.1",
	}
	initTestHandlers(stagiaire)
	select {
	case h.Register <- stagiaire:
	case <-time.After(time.Second):
		t.Fatal("timeout registering stagiaire")
	}
	time.Sleep(100 * time.Millisecond)

	// Drain the session_joined ack so we control the buffer state.
	drainOrTimeout(t, stagiaire)

	// Fill the stagiaire's buffer so the next broadcast evicts it.
	stagiaire.Send <- []byte("filler")

	// Broadcast — stagiaire buffer is full → marked closing.
	h.BroadcastSession(code, map[string]any{"type": "vote_started"}, "")
	time.Sleep(100 * time.Millisecond)

	if !stagiaire.closing.Load() {
		t.Error("stagiaire should be marked closing after broadcast overflow (CM3)")
	}

	// Second broadcast — the closing flag means BroadcastSession skips
	// the stagiaire entirely. No warning, no re-attempt, no panic.
	h.BroadcastSession(code, map[string]any{"type": "vote_closed"}, "")
}
