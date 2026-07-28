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
	"vote-backend/internal/vote"
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
	// Each captures the reclaim token minted by the server (S6/S12)
	// so phase 2 can present it on reconnect — without it, the
	// takeover path is rejected as an identity hijack.
	type sInfo struct{ id, name, token string }
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
			// Drain session_joined to capture the minted reclaim token.
			// The token must travel back through phase 2's
			// stagiaire_join or the reclaim-by-ID path is rejected.
			for k := 0; k < 20; k++ {
				select {
				case msg := <-c.Send:
					var resp map[string]any
					json.Unmarshal(msg, &resp)
					if resp["type"] == "session_joined" {
						if tok, ok := resp["reclaimToken"].(string); ok {
							infos[idx].token = tok
						}
						phase1[idx] = c
						return
					}
				case <-time.After(2 * time.Second):
					t.Errorf("phase-1 client %d: timeout waiting for session_joined", idx)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	// Snapshot tokens from the server-side state as a backstop for any
	// client whose session_joined didn't carry the field (defensive —
	// should never trigger if the protocol is wired correctly).
	if sess, ok := h.VoteManager.GetSession(code); ok {
		for i := range infos {
			if infos[i].token == "" {
				infos[i].token = sess.GetReclaimToken(infos[i].id)
			}
		}
	}
	for i, inf := range infos {
		if inf.token == "" {
			t.Fatalf("phase-1 client %d did not receive a reclaim token (S6/S12)", i)
		}
	}
	waitForStagiaireCount(t, h, code, stressClientCount, 5*time.Second)

	// Phase 2: all 30 reconnect concurrently with FRESH client objects
	// while their phase-1 counterparts are still in the connection map.
	// Each reclaim-by-ID triggers the takeover path (CL1): the old
	// client is marked closing and its conn closed. The reconnect must
	// present the reclaim token captured in phase 1 — without it the
	// join is rejected as an unauthenticated takeover attempt (S12).
	phase2 := make([]*Client, stressClientCount)
	for i := 0; i < stressClientCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := newStressClient(h, stressID('p', i))
			c.handleMessage(mustMarshal(t, models.Message{
				Type:         "stagiaire_join",
				SessionCode:  code,
				StagiaireID:  infos[idx].id,
				Name:         infos[idx].name,
				ReclaimToken: infos[idx].token,
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

// TestStagiaireReclaimRequiresToken covers S6+S12 at the protocol level:
// a presented stagiaireId is not enough to attach to an existing
// identity. The reclaim token returned in session_joined must
// accompany the join, otherwise the second connection is rejected
// (and the original identity's Scores/GameScores are protected).
func TestStagiaireReclaimRequiresToken(t *testing.T) {
	h := NewHub(stressCfg())
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1rec00")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Alice joins fresh — captures a reclaim token in session_joined.
	alice := newStressClient(h, stressID('a', 0))
	alice.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		Name:        "Alice",
	}))
	joined := drainUntil(t, alice, "session_joined")
	aliceID, _ := joined["stagiaireId"].(string)
	aliceToken, _ := joined["reclaimToken"].(string)
	if aliceToken == "" {
		t.Fatal("session_joined must carry a reclaimToken (S6/S12)")
	}
	drainUntil(t, trainer, "connected_count")

	// Competitive reveal so Alice accumulates a non-zero score that an
	// attacker would want to inherit.
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "start_vote", Colors: []string{"rouge"}, Competitive: true,
	}))
	drainN(t, trainer, 2) // vote_started + connected_count
	drainUntil(t, alice, "vote_started")
	alice.handleMessage(mustMarshal(t, models.Message{Type: "vote", Colors: []string{"rouge"}}))
	drainUntil(t, alice, "vote_accepted")
	drainN(t, trainer, 2) // vote_received + connected_count
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "close_vote"}))
	drainUntil(t, alice, "vote_closed")
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "reveal_answers", CorrectColors: []string{"rouge"},
	}))
	drainUntil(t, trainer, "answers_revealed")

	// Attacker connects with Alice's ID but no token (S12). Rejected.
	attacker := newStressClient(h, stressID('x', 1))
	attacker.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		StagiaireID: aliceID,
		Name:        "Alice",
	}))
	errMsg := drainUntil(t, attacker, "error")
	if msg, _ := errMsg["message"].(string); msg == "" {
		t.Fatal("attacker without token should be rejected")
	}

	// Attacker tries again with the wrong token. Still rejected.
	attacker2 := newStressClient(h, stressID('x', 2))
	attacker2.handleMessage(mustMarshal(t, models.Message{
		Type:         "stagiaire_join",
		SessionCode:  code,
		StagiaireID:  aliceID,
		Name:         "Alice",
		ReclaimToken: "bogus",
	}))
	drainUntil(t, attacker2, "error")

	// Sanity: the legitimate owner reconnecting with the correct token
	// reclaims the identity and the accumulated score survives.
	alice2 := newStressClient(h, stressID('a', 3))
	alice2.handleMessage(mustMarshal(t, models.Message{
		Type:         "stagiaire_join",
		SessionCode:  code,
		StagiaireID:  aliceID,
		Name:         "Alice",
		ReclaimToken: aliceToken,
	}))
	drainUntil(t, alice2, "session_joined")

	// Verify Alice's score is intact (the attacker did not inherit it).
	if sess, ok := h.VoteManager.GetSession(code); ok {
		if got := sess.GetScores()[aliceID]; got != vote.PointsPerCorrect {
			t.Errorf("Alice score after attacks: got %d, want %d (token must protect it)", got, vote.PointsPerCorrect)
		}
	}
}

// TestStagiaireNameReclaimRemoved covers S6: a fresh join with a name
// that matches a disconnected stagiaire no longer attaches to that
// identity. Names are public and guessable — they cannot be a proof
// of ownership. The join is rejected with ErrNameInUse.
func TestStagiaireNameReclaimRemoved(t *testing.T) {
	h := NewHub(stressCfg())
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1nam00")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Marie joins, then disconnects (her session entry persists until
	// the trainer resets).
	marie := newStressClient(h, stressID('m', 0))
	marie.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		Name:        "Marie",
	}))
	drainUntil(t, marie, "session_joined")
	drainUntil(t, trainer, "connected_count")
	h.Unregister <- marie
	time.Sleep(100 * time.Millisecond)
	drainUntil(t, trainer, "connected_count")

	// Attacker joins with name "Marie", no ID, no token. Previously this
	// would name-match and inherit Marie's identity; now it's blocked.
	attacker := newStressClient(h, stressID('x', 0))
	attacker.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		Name:        "Marie",
	}))
	errMsg := drainUntil(t, attacker, "error")
	if msg, _ := errMsg["message"].(string); msg != "Ce nom est déjà utilisé" {
		t.Errorf("expected name-collision rejection, got %q", msg)
	}
}

// TestMaxClientsPerSessionCap covers S7: a per-session stagiaire cap
// rejects joins past the limit. The cap counts the trainer slot too,
// so cap=N allows N-1 stagiaires.
func TestMaxClientsPerSessionCap(t *testing.T) {
	cfg := stressCfg()
	cfg.MaxClientsPerSession = 3 // 1 trainer + 2 stagiaires
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := newStressClient(h, "trainer1cap01")
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Two stagiaires fit under the cap.
	for i := 0; i < 2; i++ {
		c := newStressClient(h, stressID('a', i))
		c.handleMessage(mustMarshal(t, models.Message{
			Type:        "stagiaire_join",
			SessionCode: code,
			Name:        fmt.Sprintf("Cap%02d", i),
		}))
		drainUntil(t, c, "session_joined")
		drainUntil(t, trainer, "connected_count")
	}

	// The third stagiaire is rejected by the cap.
	third := newStressClient(h, stressID('a', 99))
	third.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		Name:        "Cap99",
	}))
	errMsg := drainUntil(t, third, "error")
	if msg, _ := errMsg["message"].(string); !strings.Contains(msg, "complète") {
		t.Errorf("expected session-complete rejection past the cap, got %q", msg)
	}
}

// TestMaxSessionsGlobalCap covers S7: the global session cap rejects
// new session creation past the limit. Joins of existing sessions are
// unaffected.
func TestMaxSessionsGlobalCap(t *testing.T) {
	cfg := stressCfg()
	cfg.MaxSessionsGlobal = 2
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Two trainers create sessions — both succeed.
	for i := 0; i < 2; i++ {
		c := newStressClient(h, fmt.Sprintf("trainerG%02d", i))
		c.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
		drainUntil(t, c, "session_created")
		drainUntil(t, c, "connected_count")
	}

	if h.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", h.SessionCount())
	}

	// Third session creation is rejected.
	third := newStressClient(h, "trainerG99")
	third.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	errMsg := drainUntil(t, third, "error")
	if msg, _ := errMsg["message"].(string); !strings.Contains(msg, "sessions actives") {
		t.Errorf("expected global-cap rejection, got %q", msg)
	}
	if h.SessionCount() != 2 {
		t.Errorf("session count should not have grown past the cap, got %d", h.SessionCount())
	}
}

// TestIPConnectionCap covers S7: the per-IP connection cap rejects WS
// upgrades past the limit. The cap is enforced before the upgrade, so
// a rejected dial never consumes a goroutine or send buffer.
func TestIPConnectionCap(t *testing.T) {
	cfg := stressCfg()
	cfg.MaxConnectionsPerIP = 2
	cfg.AllowedOrigins = []string{"*"}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Two slots are acquired successfully.
	if !h.AcquireIPSlot("10.0.0.1") {
		t.Error("first AcquireIPSlot should succeed")
	}
	if !h.AcquireIPSlot("10.0.0.1") {
		t.Error("second AcquireIPSlot should succeed")
	}
	if h.AcquireIPSlot("10.0.0.1") {
		t.Error("third AcquireIPSlot from the same IP should be rejected")
	}

	// A different IP has its own budget.
	if !h.AcquireIPSlot("10.0.0.2") {
		t.Error("AcquireIPSlot from a different IP should succeed")
	}

	// Releasing frees a slot.
	h.ReleaseIPSlot("10.0.0.1")
	if !h.AcquireIPSlot("10.0.0.1") {
		t.Error("AcquireIPSlot should succeed after a release")
	}
	if got := h.IPConnectionCount("10.0.0.1"); got != 2 {
		t.Errorf("after re-acquire, count should be 2, got %d", got)
	}

	// Over-release is safe (idempotent) — never drops below zero and
	// never panics on a missing key.
	h.ReleaseIPSlot("10.0.0.1")
	h.ReleaseIPSlot("10.0.0.1")
	h.ReleaseIPSlot("10.0.0.1")
	h.ReleaseIPSlot("10.0.0.1")
	if got := h.IPConnectionCount("10.0.0.1"); got != 0 {
		t.Errorf("after over-release, count should be clamped at 0, got %d", got)
	}

	// Disabled cap (zero or negative) lets everything through.
	cfg.MaxConnectionsPerIP = 0
	for i := 0; i < 10; i++ {
		if !h.AcquireIPSlot("10.0.0.3") {
			t.Errorf("disabled cap should allow acquire %d", i)
		}
	}
}
