package hub

import (
	"fmt"
	"testing"
	"time"
	"vote-backend/internal/config"
	"vote-backend/internal/models"
	"vote-backend/internal/vote"
)

func TestNewHub(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
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
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Fake trainer client - use 12-char lowercase alphanumeric ID matching GenerateID format
	trainer := &Client{
		ID:        "trainer1abcde",
		SessionID: "ABC",
		Type:      "trainer",
		Send:      make(chan []byte, 10),
		Hub:       h,
	}

	// Register trainer
	h.Register <- trainer

	// Wait for registration
	time.Sleep(10 * time.Millisecond)

	if !h.SessionExists("ABC") {
		t.Error("Session should exist")
	}

	// Fake stagiaire - use 12-char lowercase alphanumeric ID matching GenerateID format
	stagiaire := &Client{
		ID:        "s1abc1234567",
		SessionID: "ABC",
		Type:      "stagiaire",
		Name:      "Bob",
		Send:      make(chan []byte, 10),
		Hub:       h,
	}

	h.Register <- stagiaire
	time.Sleep(10 * time.Millisecond)

	// Check connections
	h.mu.RLock()
	conns := h.Connections["ABC"]
	h.mu.RUnlock()

	if _, ok := conns.Stagiaires["s1abc1234567"]; !ok {
		t.Error("Stagiaire should be connected")
	}

	// Unregister
	h.Unregister <- stagiaire
	time.Sleep(10 * time.Millisecond)

	h.mu.RLock()
	conns = h.Connections["ABC"]
	h.mu.RUnlock()

	if _, ok := conns.Stagiaires["s1abc1234567"]; ok {
		t.Error("Stagiaire should be disconnected")
	}
}

func TestHubCleanup(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  10 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
		PingInterval:    time.Hour,
	}
	h := NewHub(cfg)

	// Create session in manager
	h.VoteManager.CreateSession("expired_session", "t1")

	// Create entry in Hub Connections
	h.Connections["expired_session"] = &SessionConnections{}

	// Start hub loops (dispatcher + cleanup). Both are tracked by the
	// Hub's WaitGroup, so defer-Shutdown will block until both exit.
	h.Run()
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

// TestBuildScoreboardUsesCompetitionRanks covers R4: buildScoreboard
// feeds trainer reconnect, so its ranks must match what RevealAnswers
// assigned at reveal (competition ranks — tied TotalScores share a
// rank, the next lower score skips). The previous ordinal loop
// (entries[i].Rank = i+1) made a tied class flip between "1er, 1er, 3e"
// at reveal and "1er, 2e, 3e" on trainer reconnect.
func TestBuildScoreboardUsesCompetitionRanks(t *testing.T) {
	// Three stagiaires: A and B tie on TotalScore, C is strictly lower.
	// Pre-sorted by (TotalScore DESC, Name ASC) — the precondition
	// AssignCompetitionRanks documents and that buildScoreboard
	// produces via sort.Slice.
	stagiaires := map[string]string{
		"s1abc1234567": "Alice",
		"s2abc1234567": "Bob",
		"s3abc1234567": "Carol",
	}
	votes := map[string][]string{"s1abc1234567": {"rouge"}}
	scores := map[string]int{
		"s1abc1234567": 2000,
		"s2abc1234567": 2000, // tied with Alice
		"s3abc1234567": 1500, // strictly lower
	}
	gameScores := map[string]int{}

	entries := buildScoreboard(stagiaires, votes, scores, gameScores)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Order: Alice (2000), Bob (2000, name tiebreak), Carol (1500).
	wantRanks := []int{1, 1, 3}
	for i, want := range wantRanks {
		if got := entries[i].Rank; got != want {
			t.Errorf("entry %d (%s) rank: got %d want %d", i, entries[i].Name, got, want)
		}
	}
	// Cross-check against the canonical helper exported for this fix.
	if !ranksMatchAssignCompetitionRanks(entries) {
		t.Error("buildScoreboard ranks diverge from vote.AssignCompetitionRanks")
	}
}

// ranksMatchAssignCompetitionRanks re-runs the canonical helper on a
// copy and asserts the ranks match. Keeps the test honest if either
// site drifts.
func ranksMatchAssignCompetitionRanks(entries []vote.ScoreEntry) bool {
	if len(entries) == 0 {
		return true
	}
	cp := make([]vote.ScoreEntry, len(entries))
	copy(cp, entries)
	for i := range cp {
		cp[i].Rank = 0
	}
	vote.AssignCompetitionRanks(cp)
	for i := range entries {
		if entries[i].Rank != cp[i].Rank {
			return false
		}
	}
	return true
}

// TestRegisterClientUpdateTrainerFailureRollsBack covers R5: if
// UpdateTrainer fails (e.g. the Manager session was reaped between
// GetSession and UpdateTrainer), the trainer must not be left
// connected-but-sessionless — that state wedges cleanupLoop (it can't
// reap the Connections entry because conns.Trainer != nil) and every
// later op returns ErrSessionNotFound. The fix falls back to
// CreateSession (matching the !ok branch) so the trainer keeps a
// working session instead of being wedged.
func TestRegisterClientUpdateTrainerFailureRollsBack(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Bootstrap a session via the normal path so Connections[code] AND
	// VoteManager.sessions both have it (the precondition for the
	// UpdateTrainer branch).
	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Clear the active trainer so the reconnect below is a recovery
	// rather than a takeover — we want to reach the GetSession/UpdateTrainer
	// branch without the active-takeover close path firing. S13: the
	// recovery path now also requires the trainer token, so seed it on
	// the reconnecting client.
	h.mu.Lock()
	h.Connections[code].Trainer = nil
	h.mu.Unlock()
	token := ""
	if sess, ok := h.VoteManager.GetSession(code); ok {
		token = sess.GetTrainerToken()
	}
	if token == "" {
		t.Fatal("missing trainer token on bootstrapped session")
	}

	// Swap UpdateTrainer to deterministically fail — simulating the
	// race where the session is reaped between GetSession and
	// UpdateTrainer. The Manager session is still present, so GetSession
	// returns ok and we take the UpdateTrainer branch.
	orig := updateTrainerFn
	updateTrainerFn = func(_ *vote.Manager, _, _ string) error {
		return vote.ErrSessionNotFound
	}
	defer func() { updateTrainerFn = orig }()

	// New trainer dials the same code. exists=true, GetSession returns
	// ok, UpdateTrainer fails → R5 fallback to CreateSession.
	reconnect := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(reconnect)
	reconnect.handleMessage(mustMarshal(t, models.Message{
		Type:         "trainer_join",
		SessionCode:  code,
		TrainerToken: token,
	}))

	// The trainer should receive session_created (the CreateSession
	// fallback succeeded) — not an error. Before R5 the trainer got
	// "Impossible de rejoindre la session" and stayed wedged.
	drainUntil(t, reconnect, "session_created")

	h.mu.RLock()
	conns := h.Connections[code]
	active := conns.Trainer
	h.mu.RUnlock()

	if active != reconnect {
		t.Fatal("R5 fallback should leave the reconnecting trainer registered")
	}
	// The whole point of R5: the Manager session must exist under the
	// trainer (recovered, not wedged). Without the fix conns.Trainer
	// was set but the Manager session was missing → every later op
	// returned ErrSessionNotFound and cleanupLoop couldn't reap.
	if _, ok := h.VoteManager.GetSession(code); !ok {
		t.Error("Manager session missing after R5 fallback — trainer is wedged")
	}
}

// TestRegisterClientUpdateTrainerAndCreateBothFailRollsBack covers the
// defensive second leg of R5: if the UpdateTrainer fallback's
// CreateSession ALSO fails, conns.Trainer is reset to nil so cleanupLoop
// can reap the orphaned Connections entry rather than wedging forever.
// CreateSession only fails on an invalid session code today, so we
// force the path by stubbing both seams.
func TestRegisterClientUpdateTrainerAndCreateBothFailRollsBack(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	h.Connections[code].Trainer = nil
	h.mu.Unlock()
	token := ""
	if sess, ok := h.VoteManager.GetSession(code); ok {
		token = sess.GetTrainerToken()
	}
	if token == "" {
		t.Fatal("missing trainer token on bootstrapped session")
	}

	// Stub UpdateTrainer to fail AND CreateSession to fail. The
	// defensive rollback (conns.Trainer = nil) is the only path that
	// avoids wedging cleanupLoop.
	origUpdate := updateTrainerFn
	updateTrainerFn = func(_ *vote.Manager, _, _ string) error {
		return vote.ErrSessionNotFound
	}
	origCreate := createSessionFn
	createSessionFn = func(_ *vote.Manager, _, _ string) (*vote.Session, error) {
		return nil, vote.ErrInvalidInput
	}
	defer func() {
		updateTrainerFn = origUpdate
		createSessionFn = origCreate
	}()

	reconnect := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(reconnect)
	reconnect.handleMessage(mustMarshal(t, models.Message{
		Type:         "trainer_join",
		SessionCode:  code,
		TrainerToken: token,
	}))

	// The trainer should receive the rollback error.
	errMsg := drainUntil(t, reconnect, "error")
	if msg, _ := errMsg["message"].(string); msg == "" {
		t.Fatal("expected a rollback error message")
	}

	h.mu.RLock()
	conns := h.Connections[code]
	h.mu.RUnlock()
	// The whole point of the defensive rollback: conns.Trainer must be
	// nil so cleanupLoop can reap the entry (the pre-R5 bug wedged it
	// forever with conns.Trainer set but no Manager session).
	if conns.Trainer != nil {
		t.Error("conns.Trainer should be nil after both UpdateTrainer and CreateSession failed")
	}
}

// TestRegisterClientRemovesPriorIDSlot covers R16: when a *Client
// re-registers under a new stagiaireId (Client.ID is mutable across
// stagiaire_join messages — reset to OriginalID then overwritten by a
// presented, known id), registerClient must delete the prior slot the
// client held under its old ID. Without the fix, conns.Stagiaires[oldID]
// pointed at the same *Client forever — inflating connected_count (a
// phantom the trainer could never clear) and leaking a
// MaxClientsPerSession slot per cycle. The cleanup runs BEFORE the cap
// check, so a single connection cycling through many IDs cannot exhaust
// the cap and block legitimate joins.
//
// registerClient is driven directly (same package) for determinism: the
// invariant under test is the hub-level slot bookkeeping, independent of
// the WS-message path's reclaim-token machinery.
func TestRegisterClientRemovesPriorIDSlot(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:       time.Hour,
		CleanupInterval:      time.Hour,
		PingInterval:         time.Second,
		MaxClientsPerSession: 4, // tight cap so a slot leak trips it fast
		ValidColors:          []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)

	trainer := &Client{
		ID:        "trainer1abcde",
		Type:      "trainer",
		SessionID: "ABC",
		Hub:       h,
		Send:      make(chan []byte, ClientSendBufferSize),
		IP:        "127.0.0.1",
	}
	h.registerClient(trainer)
	drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)

	// Stagiaire registers under its server-minted OriginalID.
	const oldID = "s1abc1234567"
	s := &Client{
		ID:         oldID,
		OriginalID: oldID,
		Type:       "stagiaire",
		Name:       "", // anonymous: isolate the slot invariant from name logic
		SessionID:  "ABC",
		Hub:        h,
		Send:       make(chan []byte, ClientSendBufferSize),
		IP:         "127.0.0.1",
	}
	h.registerClient(s)
	drainUntilQuiet(s.Send, 20*time.Millisecond, time.Second)
	drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)

	assertStagiaireSlots := func(wantIDs ...string) {
		t.Helper()
		h.mu.RLock()
		c := h.Connections["ABC"]
		gotCount := len(c.Stagiaires)
		missing := []string{}
		for _, id := range wantIDs {
			if ptr, ok := c.Stagiaires[id]; !ok || ptr != s {
				missing = append(missing, id)
			}
		}
		// Confirm no other slots point at s.
		extras := 0
		for id, ptr := range c.Stagiaires {
			if ptr == s {
				matched := false
				for _, w := range wantIDs {
					if id == w {
						matched = true
						break
					}
				}
				if !matched {
					extras++
				}
			}
		}
		h.mu.RUnlock()
		if len(missing) != 0 {
			t.Errorf("missing expected slots for %v", missing)
		}
		if extras != 0 {
			t.Errorf("found %d extra slots still pointing at the re-registering client", extras)
		}
		if gotCount != len(wantIDs) {
			t.Errorf("connected_count: got %d, want %d", gotCount, len(wantIDs))
		}
	}

	assertStagiaireSlots(oldID)

	// The SAME *Client re-joins under a different ID (ID B is fresh to
	// the session, so JoinStagiaire takes the fresh-join path). R16 must
	// drop the oldID slot; the client now occupies exactly one slot.
	const newID = "s2abc1234567"
	s.ID = newID
	h.registerClient(s)
	drainUntilQuiet(s.Send, 20*time.Millisecond, time.Second)
	drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)
	assertStagiaireSlots(newID)

	// Cycle the same *Client through many more IDs than the cap allows
	// stagiaires (cap=4 → effective stagiaire limit 3). Without R16 each
	// cycle left a phantom slot and the 4th register would be rejected
	// with "Session complète"; with R16 the slot is reused every cycle.
	for i := 3; i <= 12; i++ {
		s.ID = fmt.Sprintf("s%011d", i) // 12-char lowercase alphanumeric, matches GenerateID shape
		h.registerClient(s)
		// A successful register yields session_joined; a cap rejection
		// yields an error. Assert we never hit the cap.
		drainUntilQuiet(s.Send, 20*time.Millisecond, time.Second)
		drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)
		select {
		case raw := <-s.Send:
			t.Fatalf("unexpected leftover message after cycle %d: %s", i, raw)
		default:
		}
		h.mu.RLock()
		count := len(h.Connections["ABC"].Stagiaires)
		h.mu.RUnlock()
		if count != 1 {
			t.Fatalf("cycle %d: connected_count should stay 1, got %d (cap exhausted by slot leak)", i, count)
		}
	}

	// Unregistering the client (readPump exit) must drop its current ID
	// and leave no phantom behind.
	h.unregisterClient(s)
	h.mu.RLock()
	leftover := len(h.Connections["ABC"].Stagiaires)
	h.mu.RUnlock()
	if leftover != 0 {
		t.Errorf("after unregister, stagiaire slots should be empty, got %d", leftover)
	}
}

// TestRegisterClientSameIDReRegisterIsNoOp covers the R16 companion
// guard: a *Client re-registering under the SAME ID is a no-op reconnect
// and must not mark-and-close its own connection (the pre-R16 block ran
// unconditionally and would self-sabotage).
func TestRegisterClientSameIDReRegisterIsNoOp(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)

	trainer := &Client{
		ID: "trainer1abcde", Type: "trainer", SessionID: "ABC", Hub: h,
		Send: make(chan []byte, ClientSendBufferSize), IP: "127.0.0.1",
	}
	h.registerClient(trainer)
	drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)

	const id = "s1abc1234567"
	s := &Client{
		ID: id, OriginalID: id, Type: "stagiaire", Name: "Alice",
		SessionID: "ABC", Hub: h,
		Send: make(chan []byte, ClientSendBufferSize), IP: "127.0.0.1",
	}
	h.registerClient(s)
	drainUntilQuiet(s.Send, 20*time.Millisecond, time.Second)
	drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)

	if s.closing.Load() {
		t.Fatal("client marked closing after first register")
	}

	// Re-register the SAME client under the SAME id with the SAME name
	// (and a valid reclaim token so JoinStagiaire takes the reclaim
	// path). The slot already belongs to s; this must be a no-op, not a
	// self-close.
	token := ""
	if sess, ok := h.VoteManager.GetSession("ABC"); ok {
		token = sess.GetReclaimToken(id)
	}
	if token == "" {
		t.Fatal("missing reclaim token")
	}
	s.ReclaimToken = token
	h.registerClient(s)
	drainUntilQuiet(s.Send, 20*time.Millisecond, time.Second)
	drainUntilQuiet(trainer.Send, 20*time.Millisecond, time.Second)

	if s.closing.Load() {
		t.Fatal("R16 self-close: client marked closing after same-id re-register")
	}

	h.mu.RLock()
	ptr := h.Connections["ABC"].Stagiaires[id]
	count := len(h.Connections["ABC"].Stagiaires)
	h.mu.RUnlock()
	if ptr != s {
		t.Error("same-id re-register should keep the same *Client in its slot")
	}
	if count != 1 {
		t.Errorf("connected_count should be 1, got %d", count)
	}
}
