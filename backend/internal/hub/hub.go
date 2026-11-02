package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/models"
	"vote-backend/internal/security"
	"vote-backend/internal/vote"
)

const (
	maxCodeRetries = 100
	maxIDRetries   = 1000

	// trainerTakeoverCloseDelay bounds how long the outgoing trainer's
	// connection is left open after a successful takeover. The warning
	// message ("New trainer connection detected…") is queued via the
	// pendingSend flush the instant h.mu is released, but the trainer's
	// writePump still needs a scheduling slice to actually drain the
	// Send channel onto the wire. Closing the conn immediately would
	// truncate the warning (S1): the displaced trainer would see a
	// bare disconnect with no clue that a new tab took over, and might
	// think the server crashed. 50ms is empirically enough for a
	// writePump parked on the Send channel to wake and flush one frame
	// on any realistic classroom network; a hard cap also bounds how
	// long the old connection lingers as a stale goroutine.
	trainerTakeoverCloseDelay = 50 * time.Millisecond
)

// updateTrainerFn is a test seam over VoteManager.UpdateTrainer so the
// R5 fallback path (UpdateTrainer fails because the Manager session was
// reaped between GetSession and UpdateTrainer) can be exercised
// deterministically without provoking a real concurrency race.
// Production never reassigns it.
var updateTrainerFn = func(m *vote.Manager, sessionID, trainerID string) error {
	return m.UpdateTrainer(sessionID, trainerID)
}

// createSessionFn is a test seam over VoteManager.CreateSession so the
// R5 defensive rollback (both UpdateTrainer and the CreateSession
// fallback fail) can be exercised deterministically. Production never
// reassigns it.
var createSessionFn = func(m *vote.Manager, sessionID, trainerID string) (*vote.Session, error) {
	return m.CreateSession(sessionID, trainerID)
}

type SessionConnections struct {
	Trainer    *Client
	Stagiaires map[string]*Client
}

// pendingSend captures a (client, message) pair deferred until after the
// hub write-lock is released (CC3). Marshalling and the channel send
// happen outside the lock so a 30-stagiaire reconnect storm doesn't
// serialise on per-message JSON work inside the critical section — the
// lock is held only for cheap pointer/map snapshots.
type pendingSend struct {
	client *Client
	msg    map[string]any
}

// pendingSends accumulates messages queued while the hub write-lock is
// held; flush() delivers them all after the lock is released (CC3).
type pendingSends struct {
	msgs []pendingSend
}

func (p *pendingSends) add(c *Client, msg map[string]any) {
	p.msgs = append(p.msgs, pendingSend{c, msg})
}

// addErr queues an error frame. code is a stable machine-readable
// identifier (vote.Code*) that clients branch on; empty means
// display-only.
func (p *pendingSends) addErr(c *Client, code, message string) {
	m := map[string]any{"type": "error", "message": message}
	if code != "" {
		m["code"] = code
	}
	p.add(c, m)
}

func (p *pendingSends) flush() {
	for _, ps := range p.msgs {
		ps.flush()
	}
}

// flush marshals and delivers the message via a direct channel send.
// The direct send bypasses the closing-flag check in SendJSON so that
// messages captured during the critical section (e.g. the takeover
// warning sent to the outgoing trainer just before it is marked
// closing) are still delivered. The closing flag protects against
// future sends from other goroutines, not against the deliberate
// flush of this batch (CL1).
//
// R24: the done case makes a post-unregister flush a clean no-op.
// Without it, a flush running after the client's readPump exited (and
// writePump followed via done) would buffer the message into Send —
// data that no live writePump will ever drain, and a misleading
// "buffer full" log if the dead channel happens to be full. Selecting
// on done (closed in readPump's defer) silently drops the message
// instead, which is correct: the conn is dead and the client is gone.
func (p pendingSend) flush() {
	data, err := json.Marshal(p.msg)
	if err != nil {
		slog.Error("Marshal error", append([]any{"error", err}, p.client.logAttrs()...)...)
		return
	}
	// R24: check done before attempting the send. A Go select with
	// multiple ready cases picks randomly, so without this priority
	// check a flush for a dead client (done closed, Send has room)
	// would sometimes deliver into Send instead of no-op-ing. The
	// check-then-send pattern guarantees no delivery when done is
	// closed; the tiny TOCTOU window (done closes between the check
	// and the send) is benign — at worst one message buffers into a
	// dead channel.
	select {
	case <-p.client.done:
		return
	default:
	}
	select {
	case p.client.Send <- data:
	default:
		slog.Warn("Pending send dropped (buffer full)", p.client.logAttrs()...)
	}
}

type Hub struct {
	Connections map[string]*SessionConnections
	VoteManager *vote.Manager
	Security    *security.Security
	Register    chan *Client
	Unregister  chan *Client

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	Config *config.Config

	// ipConns (S7) counts live WS connections per RemoteAddr so an
	// inbound flood from one IP (botnet, broken client in a reconnect
	// loop, scripted enumeration) can be rejected before the upgrade
	// completes. Incremented synchronously by AcquireIPSlot at WS
	// handshake time so concurrent dials cannot all race past the cap;
	// decremented by unregisterClient when the client's readPump exits.
	// Trainer and stagiaire connections both count — they consume the
	// same per-connection resources (goroutines, send buffer, fd).
	ipConns map[string]int

	// wg tracks every long-lived goroutine the Hub owns or watches:
	// Run, cleanupLoop, and each client's readPump/writePump. Shutdown
	// signals all of them via ctx/Conn.Close, then Wait returns once
	// every one has unregistered from the hub and torn itself down
	// (CM1+CM2: previously Shutdown only cancelled the context and
	// returned immediately, leaving hijacked WS conns un-drained and
	// the process able to exit mid-writePump).
	wg sync.WaitGroup

	// takeoverTimers holds pending delayed trainer-takeover close
	// timers so Shutdown can stop them. Without this, a timer armed
	// during a takeover that races with Shutdown would fire after
	// Shutdown returned — the one goroutine-like task the wg does not
	// track. Stop is safe: Shutdown closes every conn first, so a
	// stopped timer's callback would have been a no-op anyway.
	takeoverTimers []*time.Timer
}

func NewHub(cfg *config.Config) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		Connections: make(map[string]*SessionConnections),
		VoteManager: vote.NewManager(),
		Security:    security.NewSecurity(ctx, cfg.MaxSessionCreations),
		Register:    make(chan *Client),
		Unregister:  make(chan *Client),
		ipConns:     make(map[string]int),
		ctx:         ctx,
		cancel:      cancel,
		Config:      cfg,
	}
}

func (h *Hub) Context() context.Context {
	return h.ctx
}

// Run starts the dispatcher loop and the idle-session reaper. It performs
// the WaitGroup bookkeeping synchronously before launching any goroutine
// (the standard Add-before-go discipline) so a concurrent Shutdown caller
// never races Wait against Add — sync.WaitGroup forbids that.
//
// Use as `h.Run()` (not `go h.Run()`): Run itself launches the goroutines
// and returns immediately. Block on Shutdown to wait for them to exit.
func (h *Hub) Run() {
	h.wg.Add(2)
	go h.runLoop()
	go h.cleanupLoop()
}

// runLoop is the register/unregister dispatcher. Launched by Run.
func (h *Hub) runLoop() {
	defer h.wg.Done()
	for {
		select {
		case client := <-h.Register:
			h.registerClient(client)
		case client := <-h.Unregister:
			h.unregisterClient(client)
		case <-h.ctx.Done():
			// Drain any in-flight register/unregister so goroutines
			// blocked on the channel don't leak, then return so
			// Run's wg entry decrements.
			for {
				select {
				case c := <-h.Register:
					h.registerClient(c)
				case c := <-h.Unregister:
					h.unregisterClient(c)
				default:
					return
				}
			}
		}
	}
}

// Shutdown signals every Hub-owned goroutine to wind down (Run,
// cleanupLoop, every client's readPump + writePump) and blocks until all
// of them have exited (CM1+CM2). Caller is expected to have already
// stopped accepting new connections (e.g. via HTTP server shutdown).
//
// Ordering matters: the context cancel is what tells Run/cleanupLoop/
// writePump to exit, and Conn.Close is what unblocks a readPump parked
// in ReadMessage. After signalling we wait once for everything — the
// per-client goroutines unregister themselves on the way out, which
// also drains pending send work.
func (h *Hub) Shutdown() {
	h.Security.Shutdown()
	h.cancel()

	// Close every live WebSocket so blocked readPumps return. markClosing
	// first so concurrent trySend/SendJSON short-circuit instead of
	// pushing onto a closing channel.
	h.mu.RLock()
	type closeable struct{ c *Client }
	var toClose []closeable
	for _, conns := range h.Connections {
		if conns.Trainer != nil && !conns.Trainer.closing.Load() {
			conns.Trainer.markClosing()
			toClose = append(toClose, closeable{conns.Trainer})
		}
		for _, c := range conns.Stagiaires {
			if !c.closing.Load() {
				c.markClosing()
				toClose = append(toClose, closeable{c})
			}
		}
	}
	h.mu.RUnlock()
	for _, cl := range toClose {
		if cl.c.Conn != nil {
			cl.c.Conn.Close()
		}
	}

	// Stop pending takeover-close timers: the mass Close above already
	// closed their target conns, so letting one fire after Shutdown
	// returns would break the "nothing hub-owned outlives Shutdown"
	// invariant for no benefit.
	h.mu.Lock()
	for _, t := range h.takeoverTimers {
		t.Stop()
	}
	h.takeoverTimers = nil
	h.mu.Unlock()

	h.wg.Wait()
}

func (h *Hub) SessionExists(sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.Connections[sessionID]
	return ok
}

// SessionCount returns the number of live session entries the Hub is
// tracking. Used by the global-session cap (S7) — Connections is a
// superset of VoteManager.sessions during normal operation because
// GenerateSessionCode reserves the entry before the trainer registers,
// and cleanupLoop only reaps entries with no session AND no clients.
func (h *Hub) SessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.Connections)
}

// AtSessionsCap reports whether the global session cap (S7) is reached.
// A zero or negative MaxSessionsGlobal disables the cap (used by tests
// and single-tenant dev where bounding isn't worth the bookkeeping).
func (h *Hub) AtSessionsCap() bool {
	if h.Config.MaxSessionsGlobal <= 0 {
		return false
	}
	return h.SessionCount() >= h.Config.MaxSessionsGlobal
}

// AcquireIPSlot increments the per-IP connection counter and reports
// whether the new connection is permitted under the cap (S7). The
// caller must pair this with a ReleaseIPSlot when the connection ends
// (readPump always sends itself to Unregister on exit, and
// unregisterClient calls ReleaseIPSlot). The increment happens under
// h.mu so concurrent dials from the same IP see each other. A zero or
// negative MaxConnectionsPerIP disables the cap.
func (h *Hub) AcquireIPSlot(ip string) bool {
	if ip == "" || h.Config.MaxConnectionsPerIP <= 0 {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ipConns[ip] >= h.Config.MaxConnectionsPerIP {
		return false
	}
	h.ipConns[ip]++
	return true
}

// ReleaseIPSlot decrements the per-IP counter (S7). Idempotent: never
// drops below zero, so a double-release (e.g. an unregistered client
// whose slot was never acquired due to a config change mid-flight) is
// safe.
func (h *Hub) ReleaseIPSlot(ip string) {
	if ip == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	n := h.ipConns[ip]
	if n <= 1 {
		// Drop the key entirely so the map doesn't accumulate zero
		// entries (a long-lived server would otherwise retain every
		// IP it ever saw). The next Acquire re-creates the entry.
		delete(h.ipConns, ip)
		return
	}
	h.ipConns[ip] = n - 1
}

// IPConnectionCount returns the current per-IP connection count.
// Test-only helper.
func (h *Hub) IPConnectionCount(ip string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ipConns[ip]
}

func (h *Hub) GenerateSessionCode() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	alphabet := []byte(vote.SessionAlphabet)
	codeLen := vote.SessionCodeLength

	// Build the used-set once (O(sessions), bounded by the global
	// session cap) so each random probe is an O(1) membership check.
	used := make(map[string]bool, len(h.Connections)+len(h.VoteManager.GetSessionIDs()))
	for id := range h.Connections {
		used[id] = true
	}
	for _, id := range h.VoteManager.GetSessionIDs() {
		used[id] = true
	}

	// Random probes. With the default code space (23^4 ≈ 280k codes)
	// and MaxSessionsGlobal 1000, the per-attempt collision
	// probability is ~0.36%, so 100 attempts succeed with overwhelming
	// probability. An empty return means the space is effectively
	// saturated and the caller reports "no codes available". A
	// deterministic exhaustive walk is deliberately NOT attempted: it
	// is exponential in codeLen and would freeze the hub lock if the
	// length were ever raised (R2).
	for i := 0; i < maxCodeRetries; i++ {
		code := make([]byte, codeLen)
		for j := 0; j < codeLen; j++ {
			code[j] = alphabet[rand.IntN(len(alphabet))] //nolint:gosec // non-crypto random for short session codes
		}
		s := string(code)
		if !used[s] {
			h.Connections[s] = &SessionConnections{Stagiaires: make(map[string]*Client)}
			return s
		}
	}
	return ""
}

func (h *Hub) GenerateUniqueClientID() (string, bool) {
	for i := 0; i < maxIDRetries; i++ {
		id := security.GenerateID()
		if !h.ClientIDExists(id) {
			return id, true
		}
	}
	return "", false
}

func (h *Hub) ClientIDExists(id string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, conn := range h.Connections {
		if conn.Trainer != nil && conn.Trainer.ID == id {
			return true
		}
		if _, ok := conn.Stagiaires[id]; ok {
			return true
		}
	}

	return h.VoteManager.StagiaireExists(id)
}

// registerClient admits one client to the hub's connection registry.
// It runs in runLoop (single-goroutine) and in cleanupLoop phase 2 —
// the only two places Connections is mutated. All sends are queued on
// p and flushed by the deferred flush AFTER the deferred h.mu.Unlock
// (LIFO), so no message is ever marshalled or sent under the hub
// write-lock (CC3).
func (h *Hub) registerClient(client *Client) {
	p := &pendingSends{}
	defer p.flush()

	h.mu.Lock()
	defer h.mu.Unlock()

	conns, exists := h.Connections[client.SessionID]
	if client.Type == "trainer" {
		h.registerTrainerLocked(client, conns, exists, p)
		return
	}
	h.registerStagiaireLocked(client, conns, p)
}

// registerTrainerLocked handles the trainer side of registration: the
// S13 token gate, connection-slot creation, session create/recover,
// takeover of an outgoing trainer, and the session_created handshake.
// Callers hold h.mu (and only h.mu); sends must go through p so they
// flush outside the lock.
func (h *Hub) registerTrainerLocked(client *Client, conns *SessionConnections, exists bool, p *pendingSends) {
	// S13 invariant: any join to an already-minted Manager session
	// requires the per-session trainer token — including the
	// empty-slot recovery path after the previous trainer's readPump
	// exited. The check runs BEFORE the !exists branch so a tokenless
	// claimant cannot bypass it via the createSessionFn path (which
	// would overwrite the minted token). Fresh session creation is the
	// only path that mints and emits a token to a previously
	// unauthenticated trainer. Rejections reuse the "Session
	// introuvable" message so the response does not leak that the code
	// is live (S14).
	if sess, ok := h.VoteManager.GetSession(client.SessionID); ok && sess.GetTrainerToken() != "" {
		if !h.VoteManager.ValidateTrainerToken(client.SessionID, client.TrainerToken) {
			p.addErr(client, vote.CodeSessionNotFound, "Session introuvable")
			return
		}
	}

	if !exists {
		conns = &SessionConnections{
			Stagiaires: make(map[string]*Client),
		}
		h.Connections[client.SessionID] = conns
		// S13 defensive guard: if a Manager session already exists
		// here (Connections entry reaped while the Manager session
		// survived — not reachable via cleanupLoop's normal contract,
		// but defense in depth), do NOT call createSessionFn: it would
		// overwrite the existing minted token. The trainer was
		// authenticated above; the existing session is theirs to
		// recover.
		if _, ok := h.VoteManager.GetSession(client.SessionID); !ok {
			if _, err := createSessionFn(h.VoteManager, client.SessionID, client.ID); err != nil {
				slog.Error("Failed to create session",
					append([]any{"error", err}, client.logAttrs()...)...)
				delete(h.Connections, client.SessionID)
				p.addErr(client, vote.CodeCreateFailed, "Impossible de créer la session")
				return
			}
		}
	}

	if old := conns.Trainer; old != nil && old != client {
		// Active takeover. The token was validated above; this branch
		// just closes the outgoing trainer's connection after a brief
		// grace period so its writePump can flush the warning (S1).
		// The timer is tracked so Shutdown can stop pending ones
		// instead of letting them fire after Shutdown returns.
		p.addErr(old, vote.CodeTrainerReplaced, "New trainer connection detected, closing this one.")
		old.markClosing()
		h.takeoverTimers = append(h.takeoverTimers, time.AfterFunc(trainerTakeoverCloseDelay, func() {
			if old.Conn != nil {
				old.Conn.Close()
			}
		}))
	}

	conns.Trainer = client
	if _, ok := h.VoteManager.GetSession(client.SessionID); !ok {
		if _, err := createSessionFn(h.VoteManager, client.SessionID, client.ID); err != nil {
			slog.Error("Failed to create session",
				append([]any{"error", err}, client.logAttrs()...)...)
			conns.Trainer = nil
			p.addErr(client, vote.CodeCreateFailed, "Impossible de créer la session")
			return
		}
	} else if err := updateTrainerFn(h.VoteManager, client.SessionID, client.ID); err != nil {
		// R5: UpdateTrainer can fail with ErrSessionNotFound when the
		// session is reaped between the GetSession check above and the
		// call. Fall back to CreateSession so the trainer keeps a
		// working session; if that also fails, roll conns.Trainer back
		// so cleanupLoop can reap the entry.
		slog.Warn("UpdateTrainer failed; falling back to CreateSession",
			append([]any{"error", err}, client.logAttrs()...)...)
		if _, cerr := createSessionFn(h.VoteManager, client.SessionID, client.ID); cerr != nil {
			slog.Error("Failed to create session after UpdateTrainer fallback",
				append([]any{"error", cerr}, client.logAttrs()...)...)
			conns.Trainer = nil
			p.addErr(client, vote.CodeCreateFailed, "Impossible de créer la session")
			return
		}
	}

	sessionCreated := map[string]any{
		"type":        "session_created",
		"sessionCode": client.SessionID,
		"trainerId":   client.ID,
	}
	// Emit the token to the trainer: every trainer reaching this point
	// is the creator of a freshly-minted session or has validated the
	// token above (S13). The client persists it (sessionStorage,
	// tab-scoped) and re-sends it on every trainer_join so reconnects
	// re-validate against the same gate.
	if sess, ok := h.VoteManager.GetSession(client.SessionID); ok {
		if token := sess.GetTrainerToken(); token != "" {
			sessionCreated["trainerToken"] = token
		}
	}
	p.add(client, sessionCreated)

	if ps := h.buildTrainerStagiaireListLocked(conns, client.SessionID, "connected_count"); ps != nil {
		p.msgs = append(p.msgs, *ps)
	}

	h.replayTrainerStateLocked(client, p)
}

// replayTrainerStateLocked brings a (re)connecting trainer up to date
// with the session's canonical state: an active/closed vote replayed
// with every recorded vote, or — on an idle but configured session —
// the config_updated snapshot (FH4) that overrides the client's
// localStorage with the server's source of truth. Callers hold h.mu.
func (h *Hub) replayTrainerStateLocked(client *Client, p *pendingSends) {
	session, ok := h.VoteManager.GetSession(client.SessionID)
	if !ok {
		return
	}
	state, colors, multipleChoice, voteStartTime := session.GetState()
	labels := session.GetActiveLabels()
	gameEnabled := session.GetGameEnabled()
	competitive := session.GetCompetitive()
	allowBlank := session.GetAllowBlank()

	switch {
	case state == models.VoteStateActive || state == models.VoteStateClosed:
		replayMsg := map[string]any{
			"type":           "vote_started",
			"colors":         colors,
			"multipleChoice": multipleChoice,
			"voteStartTime":  voteStartTime,
			"voteElapsed":    time.Now().Unix() - voteStartTime,
			"gameEnabled":    gameEnabled,
			"competitive":    competitive,
			"allowBlank":     allowBlank,
		}
		if labels != nil {
			replayMsg["labels"] = labels
		}
		p.add(client, replayMsg)

		votes := session.GetVotes()
		stagiaires := session.GetStagiaires()
		for sID, vColors := range votes {
			p.add(client, map[string]any{
				"type":          "vote_received",
				"stagiaireId":   sID,
				"stagiaireName": stagiaires[sID],
				"colors":        vColors,
			})
		}

		if state == models.VoteStateClosed {
			p.add(client, map[string]any{"type": "vote_closed"})
		}

		if competitive && session.GetRevealed() {
			scores := session.GetScores()
			gameScores := session.GetGameScores()
			correctColors := session.GetCorrectColors()
			p.add(client, map[string]any{
				"type":          "answers_revealed",
				"correctColors": correctColors,
				"scores":        buildScoreboard(stagiaires, votes, scores, gameScores),
			})
		}
	case len(colors) > 0:
		// Only sync config when the session has been configured; on a
		// fresh session empty colors would clobber the client's
		// autoloaded last-config.
		replayMsg := map[string]any{
			"type":           "config_updated",
			"selectedColors": colors,
			"multipleChoice": multipleChoice,
			"gameEnabled":    gameEnabled,
			"competitive":    competitive,
			"allowBlank":     allowBlank,
		}
		if labels != nil {
			replayMsg["labels"] = labels
		}
		p.add(client, replayMsg)
	}
}

// registerStagiaireLocked handles the stagiaire side of registration:
// the trainer-presence check, the duplicate-slot sweep (R16), the
// per-session cap (S7), the authoritative JoinStagiaire call, slot
// takeover by ID, and the session_joined handshake. Callers hold h.mu.
func (h *Hub) registerStagiaireLocked(client *Client, conns *SessionConnections, p *pendingSends) {
	if conns == nil {
		p.addErr(client, vote.CodeSessionNotFound, "Session introuvable")
		return
	}
	if conns.Trainer == nil {
		p.addErr(client, vote.CodeNoTrainer, "Session indisponible (aucun formateur connecté)")
		return
	}

	// R16: a *Client that re-joins under a different stagiaireId would
	// otherwise leave its prior slot pointing at the same pointer
	// forever — inflating connected_count and leaking a
	// MaxClientsPerSession slot per cycle. The scan runs BEFORE the cap
	// check so the count reflects reality; O(N) with N bounded by the
	// cap (the common case has zero matches).
	for id, c := range conns.Stagiaires {
		if c == client && id != client.ID {
			delete(conns.Stagiaires, id)
		}
	}

	// S7: per-session client cap. Counts the trainer slot too so the
	// effective stagiaire limit is cap-1. Zero or negative config
	// disables the cap (used by tests that don't care about the bound).
	if h.Config.MaxClientsPerSession > 0 && len(conns.Stagiaires)+1 >= h.Config.MaxClientsPerSession {
		p.addErr(client, vote.CodeSessionFull, "Session complète — réessayez plus tard")
		return
	}

	result, err := h.VoteManager.JoinStagiaire(client.SessionID, client.ID, client.Name, client.ReclaimToken)
	if err != nil {
		// CC2: JoinStagiaire is the authoritative name-uniqueness and
		// reclaim-token arbiter (under the session lock, constant-time
		// compare); B3 keeps the wire French with no internal entity
		// names leaked.
		p.addErr(client, vote.ErrorCode(err), vote.UserFacingError(err))
		return
	}

	// CL1: flag an outgoing client that held this same ID so in-flight
	// broadcasts drop it. R16: skip when the slot already belongs to
	// this same *Client (a same-ID re-register is a no-op reconnect
	// and must not close the client's own conn).
	if old, ok := conns.Stagiaires[client.ID]; ok && old != client {
		old.markClosing()
		if old.Conn != nil {
			old.Conn.Close()
		}
	}
	conns.Stagiaires[client.ID] = client

	// S6/S12: surface the reclaim token so the client can prove
	// ownership of this identity on future reconnects. Without it, a
	// stale stagiaireId alone cannot inherit the prior Scores /
	// GameScores.
	p.add(client, map[string]any{
		"type":         "session_joined",
		"sessionCode":  client.SessionID,
		"stagiaireId":  client.ID,
		"reclaimToken": result.ReclaimToken,
	})

	if ps := h.buildTrainerStagiaireListLocked(conns, client.SessionID, "connected_count"); ps != nil {
		p.msgs = append(p.msgs, *ps)
	}

	h.replayStagiaireStateLocked(client, p)
}

// replayStagiaireStateLocked brings a (re)connecting stagiaire up to
// date: an active vote (with their existing vote, if any) or a closed
// one plus their personal reveal. Callers hold h.mu.
func (h *Hub) replayStagiaireStateLocked(client *Client, p *pendingSends) {
	session, ok := h.VoteManager.GetSession(client.SessionID)
	if !ok {
		return
	}
	state, colors, multipleChoice, _ := session.GetState()
	gameEnabled := session.GetGameEnabled()
	competitive := session.GetCompetitive()
	allowBlank := session.GetAllowBlank()
	switch state {
	case models.VoteStateActive:
		msg := map[string]any{
			"type":           "vote_started",
			"colors":         colors,
			"multipleChoice": multipleChoice,
			"gameEnabled":    gameEnabled,
			"competitive":    competitive,
			"allowBlank":     allowBlank,
		}
		if existingVote, hasVoted := session.GetVote(client.ID); hasVoted {
			msg["existingVote"] = existingVote
		}
		p.add(client, msg)
	case models.VoteStateClosed:
		msg := map[string]any{
			"type":           "vote_started",
			"colors":         colors,
			"multipleChoice": multipleChoice,
			"gameEnabled":    gameEnabled,
			"competitive":    competitive,
			"allowBlank":     allowBlank,
		}
		if existingVote, hasVoted := session.GetVote(client.ID); hasVoted {
			msg["existingVote"] = existingVote
		}
		p.add(client, msg)
		p.add(client, map[string]any{"type": "vote_closed"})
		if competitive && session.GetRevealed() {
			correctColors := session.GetCorrectColors()
			scores := session.GetScores()
			gameScores := session.GetGameScores()
			stagiaires := session.GetStagiaires()
			rank, total := computeRank(scores, gameScores, stagiaires, client.ID)
			p.add(client, map[string]any{
				"type":            "answers_revealed",
				"correctColors":   correctColors,
				"totalScore":      scores[client.ID],
				"gameScore":       gameScores[client.ID],
				"rank":            rank,
				"totalStagiaires": total,
			})
		}
	}
}

func (h *Hub) unregisterClient(client *Client) {
	var pending []pendingSend

	// S7: release the IP slot regardless of whether the client was
	// registered. AcquireIPSlot was called at WS upgrade time, so every
	// readPump defer (which always routes here) pairs with exactly one
	// acquire — even if the client never sent a join message. Registered
	// FIRST so it runs LAST (LIFO) — after both the unlock defer and the
	// pending-sends flush. Taking h.mu inside ReleaseIPSlot while the
	// function's own h.mu.Unlock() has already run is safe; doing it
	// before that unlock would self-deadlock.
	defer h.ReleaseIPSlot(client.IP)

	// Deferred flush runs AFTER the unlock defer (LIFO): sends happen
	// outside the hub write-lock (CC3).
	defer func() {
		for _, p := range pending {
			p.flush()
		}
	}()

	h.mu.Lock()
	defer h.mu.Unlock()

	conns, exists := h.Connections[client.SessionID]
	if !exists {
		return
	}

	if client.Type == "trainer" {
		if conns.Trainer == client {
			conns.Trainer = nil
		}
	} else {
		if conns.Stagiaires[client.ID] == client {
			delete(conns.Stagiaires, client.ID)
			if ps := h.buildTrainerStagiaireListLocked(conns, client.SessionID, "connected_count"); ps != nil {
				pending = append(pending, *ps)
			}
		}
	}
}

// BroadcastSession sends a message to every client in a session except the
// one identified by excludeID (which may be ""). B11: the existence check
// and target snapshot happen under RLock before the payload is marshalled,
// so a broadcast aimed at a dead or never-existing session pays no marshal
// cost — relevant for the per-vote connected_count fanout under reconnect
// storms where the manager may have already reaped the session.
func (h *Hub) BroadcastSession(sessionID string, message any, excludeID string) {
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	if !exists {
		h.mu.RUnlock()
		return
	}

	var targets []*Client
	if conns.Trainer != nil && conns.Trainer.ID != excludeID {
		targets = append(targets, conns.Trainer)
	}
	for id, client := range conns.Stagiaires {
		if id != excludeID {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	data, err := json.Marshal(message)
	if err != nil {
		slog.Error("Marshal error", "session", sessionID, "error", err)
		return
	}

	for _, c := range targets {
		if c.closing.Load() {
			continue
		}
		// trySend applies the role-aware buffer-full policy (CC1):
		// stagiaires are evicted (and marked closing so the next
		// broadcast in the same storm skips them instead of warn-
		// spamming until pongWait — CM3); the trainer gets a dropped
		// message rather than a torn-down control surface.
		c.trySend(data)
	}
}

func (h *Hub) SendToTrainer(sessionID string, message any) {
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	var trainer *Client
	if exists && conns.Trainer != nil {
		trainer = conns.Trainer
	}
	h.mu.RUnlock()

	if trainer != nil {
		trainer.SendJSON(message)
	}
}

func (h *Hub) SendScoreReveal(sessionID string, correctColors []string, entries []vote.ScoreEntry) {
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	if !exists {
		h.mu.RUnlock()
		return
	}

	session, ok := h.VoteManager.GetSession(sessionID)
	if !ok {
		h.mu.RUnlock()
		return
	}
	gameScores := session.GetGameScores()

	type target struct {
		client *Client
		entry  vote.ScoreEntry
	}
	targets := make([]target, 0, len(conns.Stagiaires))
	for id, client := range conns.Stagiaires {
		for _, e := range entries {
			if e.StagiaireID == id {
				targets = append(targets, target{client, e})
				break
			}
		}
	}
	total := len(entries)
	h.mu.RUnlock()

	for _, t := range targets {
		gameScore := gameScores[t.entry.StagiaireID]
		t.client.SendJSON(map[string]any{
			"type":            "answers_revealed",
			"correctColors":   correctColors,
			"voteScore":       t.entry.VoteScore,
			"totalScore":      t.entry.TotalScore - gameScore,
			"gameScore":       gameScore,
			"rank":            t.entry.Rank,
			"totalStagiaires": total,
		})
	}
}

func (h *Hub) NotifyTrainerStagiaireList(sessionID string, msgType string) {
	// CC3: build the message under the read-lock, release, then flush
	// (marshal + channel send) outside the lock so a burst of these
	// (one per stagiaire in a reconnect storm) doesn't serialise on
	// per-call JSON work.
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	var ps *pendingSend
	if exists {
		ps = h.buildTrainerStagiaireListLocked(conns, sessionID, msgType)
	}
	h.mu.RUnlock()

	if ps != nil {
		ps.flush()
	}
}

// buildTrainerStagiaireListLocked constructs the connected_count /
// stagiaire_names_updated payload. It snapshots session state (cheap
// map copies under session.mu) but deliberately does NOT marshal or
// send — the caller flushes the returned pendingSend after releasing
// h.mu so the marshal work stays outside the hub lock (CC3).
func (h *Hub) buildTrainerStagiaireListLocked(conns *SessionConnections, sessionID string, msgType string) *pendingSend {
	if conns.Trainer == nil {
		return nil
	}

	session, ok := h.VoteManager.GetSession(sessionID)
	if !ok {
		return nil
	}

	stagiaires := session.GetStagiaires()
	votes := session.GetVotes()
	scores := session.GetScores()
	gameScores := session.GetGameScores()
	competitive := session.GetCompetitive()

	type StagiaireInfo struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Connected bool     `json:"connected"`
		Vote      []string `json:"vote,omitempty"`
		Score     int      `json:"score,omitempty"`
		GameScore int      `json:"gameScore,omitempty"`
	}

	list := make([]StagiaireInfo, 0, len(stagiaires))
	for id, name := range stagiaires {
		_, connected := conns.Stagiaires[id]
		vote, hasVoted := votes[id]
		info := StagiaireInfo{
			ID:        id,
			Name:      name,
			Connected: connected,
		}
		if hasVoted {
			info.Vote = vote
		}
		if competitive {
			info.Score = scores[id]
			info.GameScore = gameScores[id]
		}
		list = append(list, info)
	}

	return &pendingSend{
		client: conns.Trainer,
		msg: map[string]any{
			"type":       msgType,
			"count":      len(conns.Stagiaires),
			"stagiaires": list,
		},
	}
}

func (h *Hub) cleanupLoop() {
	defer h.wg.Done()
	ticker := time.NewTicker(h.Config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// R26: hold h.mu.RLock across the protected-snapshot AND the
			// reap so the two are atomic w.r.t. register/unregister (which
			// need h.mu.Lock). A session published in the window between
			// the old snapshot-then-RUnlock and CleanupExpiredSessions was
			// not in `protected`; if its LastActivity read as stale (clock
			// skew, a long STW pause) it was reaped out from under the
			// trainer. Lock ordering is h.mu → m.mu → session.mu (the
			// established invariant), so nesting m.mu.Lock inside
			// h.mu.RLock is safe. Cleanup runs at CleanupInterval, not on
			// the hot path, so the brief serialization with register is
			// acceptable.
			h.mu.RLock()
			protected := make(map[string]bool)
			for id, conns := range h.Connections {
				if conns.Trainer != nil || len(conns.Stagiaires) > 0 {
					protected[id] = true
				}
			}

			h.VoteManager.CleanupExpiredSessions(h.Config.SessionTimeout, protected)
			h.mu.RUnlock()

			h.mu.Lock()
			for id, conns := range h.Connections {
				if _, ok := h.VoteManager.GetSession(id); ok {
					continue
				}
				if conns.Trainer == nil && len(conns.Stagiaires) == 0 {
					delete(h.Connections, id)
				}
			}
			h.mu.Unlock()
		case <-h.ctx.Done():
			return
		}
	}
}

func buildScoreboard(stagiaires map[string]string, votes map[string][]string, scores map[string]int, gameScores map[string]int) []vote.ScoreEntry {
	entries := make([]vote.ScoreEntry, 0, len(stagiaires))
	for id, name := range stagiaires {
		entry := vote.ScoreEntry{
			StagiaireID: id,
			Name:        name,
			TotalScore:  scores[id] + gameScores[id],
		}
		if v, ok := votes[id]; ok {
			entry.Vote = v
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TotalScore != entries[j].TotalScore {
			return entries[i].TotalScore > entries[j].TotalScore
		}
		return entries[i].Name < entries[j].Name
	})
	// R4: competition ranks (tied TotalScores share a rank, the next
	// lower score skips) so a reconnecting trainer sees the same ranks
	// that RevealAnswers assigned at reveal. The previous ordinal loop
	// (i+1) made a tied class flip between "1er, 1er, 3e" at reveal and
	// "1er, 2e, 3e" on trainer reconnect.
	vote.AssignCompetitionRanks(entries)
	return entries
}

func computeRank(voteScores, gameScores map[string]int, stagiaires map[string]string, id string) (int, int) {
	// C1: total (and the rank denominator) is derived from the live
	// identity set, not the at-reveal Scores snapshot. A stagiaire
	// joining a Competitive session during Closed+Revealed is added to
	// session.Stagiaires but not to session.Scores (Scores is populated
	// only inside RevealAnswers' Stagiaires iteration). The previous
	// implementation set total = len(voteScores) = N (pre-reveal class
	// size, excluding the joiner) and counted rank only over existing
	// scorers, so a scoreless joiner with everyone else positive got
	// rank = N+1 of N — an impossible "rank exceeds total" the classroom
	// saw until the next reveal normalised it. Iterating the live
	// stagiaires map makes total reflect the current class size and
	// ranks the joiner (score 0) correctly against everyone, including
	// other 0-scorers they tie with.
	total := len(stagiaires)
	if total == 0 {
		return 0, 0
	}
	myScore := voteScores[id] + gameScores[id]
	rank := 1
	for otherID := range stagiaires {
		if otherID == id {
			continue
		}
		if voteScores[otherID]+gameScores[otherID] > myScore {
			rank++
		}
	}
	return rank, total
}

type Metrics struct {
	ActiveSessions      int            `json:"active_sessions"`
	ConnectedTrainers   int            `json:"connected_trainers"`
	ConnectedStagiaires int            `json:"connected_stagiaires"`
	VoteStates          map[string]int `json:"vote_states"`
}

func (h *Hub) GetMetrics() Metrics {
	// B8: the previous implementation held h.mu.RLock across the entire
	// scrape, including up to MaxSessionsGlobal (default 1000)
	// session.GetState() calls — each takes the per-session lock. A
	// Prometheus scrape therefore blocked every register/unregister
	// (and the per-vote BroadcastSession fanout, which needs RLock).
	// Snapshot the Connections-derived counters and the session pointer
	// slice under RLock, release, then iterate lock-free. The session
	// slice comes from VoteManager.GetAllSessions, which already takes
	// and releases the manager RLock internally; GetState takes only
	// the per-session lock, so the lock-free iteration is safe.
	h.mu.RLock()
	metrics := Metrics{
		ActiveSessions:      len(h.Connections),
		ConnectedTrainers:   0,
		ConnectedStagiaires: 0,
		VoteStates: map[string]int{
			models.VoteStateIdle:   0,
			models.VoteStateActive: 0,
			models.VoteStateClosed: 0,
		},
	}

	for _, conn := range h.Connections {
		if conn.Trainer != nil {
			metrics.ConnectedTrainers++
		}
		metrics.ConnectedStagiaires += len(conn.Stagiaires)
	}
	h.mu.RUnlock()

	sessions := h.VoteManager.GetAllSessions()
	for _, session := range sessions {
		state, _, _, _ := session.GetState()
		metrics.VoteStates[state]++
	}

	return metrics
}

// ProductStats returns the aggregate usage counters (sessions, votes,
// trainees, feature adoption) collected by the vote Manager. Exposed via the
// /metrics endpoint for maintainer insights.
func (h *Hub) ProductStats() vote.ProductStatsSnapshot {
	return h.VoteManager.Stats().Snapshot()
}
