package hub

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/models"
)

// TestBroadcastSessionSkipsDeadSessionWithoutMarshal covers B11: a broadcast
// aimed at a dead or never-existing session must short-circuit before the
// payload is marshalled. The per-vote connected_count fanout runs through
// here on every vote; without the early-return, dead-session broadcasts
// pay full JSON marshal cost on the hot path.
//
// We assert the no-marshal property by passing a value whose MarshalJSON
// flips a counter; if BroadcastSession early-returns, the counter stays
// zero.
type marshalCountingValue struct {
	calls *atomic.Int64
}

func (m marshalCountingValue) MarshalJSON() ([]byte, error) {
	m.calls.Add(1)
	return json.Marshal(map[string]any{"type": "test"})
}

func TestBroadcastSessionSkipsDeadSessionWithoutMarshal(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	var calls atomic.Int64
	payload := marshalCountingValue{calls: &calls}

	// Unknown session: must not marshal, must not block.
	h.BroadcastSession("NOPE", payload, "")
	if got := calls.Load(); got != 0 {
		t.Errorf("BroadcastSession to unknown session marshalled %d times (expected 0 — B11)", got)
	}

	// Sanity: a real session does marshal exactly once.
	trainer := newStressClient(h, "trainer1bs001")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	h.BroadcastSession(code, payload, "")
	if got := calls.Load(); got != 1 {
		t.Errorf("BroadcastSession to live session should marshal once, got %d", got)
	}

	// Exclude the trainer: with no other clients, there are no targets,
	// so the second marshal is wasted work that B11 also skips via the
	// len(targets)==0 fast path.
	calls.Store(0)
	h.BroadcastSession(code, payload, trainer.ID)
	if got := calls.Load(); got != 0 {
		t.Errorf("BroadcastSession with no targets should not marshal, got %d (B11 fast path)", got)
	}
}

// TestBroadcastSessionExcludedClientNotDelivered covers the B11 reorder
// combined with the existing excludeID contract: even when other clients
// ARE present, the excluded one must not receive the broadcast.
func TestBroadcastSessionExcludedClientNotDelivered(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1exc001")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")

	alice := newStressClient(h, "aliceexc0001")
	alice.handleMessage(mustMarshal(t, models.Message{
		Type: "stagiaire_join", SessionCode: code, Name: "Alice",
	}))
	drainUntil(t, alice, "session_joined")
	drainUntil(t, trainer, "connected_count")

	bob := newStressClient(h, "bobexc000001")
	bob.handleMessage(mustMarshal(t, models.Message{
		Type: "stagiaire_join", SessionCode: code, Name: "Bob",
	}))
	drainUntil(t, bob, "session_joined")
	drainUntil(t, trainer, "connected_count")

	// Exclude Alice: she must NOT receive the broadcast, Bob + trainer do.
	h.BroadcastSession(code, map[string]any{"type": "test-bcast"}, alice.ID)

	// Drain everything that arrives; only Alice's channel must stay empty.
	for _, c := range []*Client{trainer, bob} {
		drainUntil(t, c, "test-bcast")
	}
	select {
	case msg := <-alice.Send:
		t.Errorf("excluded client received the broadcast: %s", string(msg))
	case <-time.After(100 * time.Millisecond):
		// expected: no message for Alice
	}
}
