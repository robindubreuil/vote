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

// flush marshals and delivers the message via a direct channel send.
// The direct send bypasses the closing-flag check in SendJSON so that
// messages captured during the critical section (e.g. the takeover
// warning sent to the outgoing trainer just before it is marked
// closing) are still delivered. The closing flag protects against
// future sends from other goroutines, not against the deliberate
// flush of this batch (CL1).
func (p pendingSend) flush() {
	data, err := json.Marshal(p.msg)
	if err != nil {
		slog.Error("Marshal error", append([]any{"error", err}, p.client.logAttrs()...)...)
		return
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

	for i := 0; i < maxCodeRetries; i++ {
		code := make([]byte, codeLen)
		for j := 0; j < codeLen; j++ {
			code[j] = alphabet[rand.IntN(len(alphabet))] //nolint:gosec // non-crypto random for short session codes
		}
		s := string(code)
		if _, exists := h.Connections[s]; !exists && !h.VoteManager.SessionExists(s) {
			h.Connections[s] = &SessionConnections{Stagiaires: make(map[string]*Client)}
			return s
		}
	}

	// Exhaustive fallback: walk the alphabet lexicographically and return the
	// first free code. Covers the (extremely unlikely) case where randomness
	// collides 100 times in a row.
	used := make(map[string]bool, len(h.Connections)+10000)
	for id := range h.Connections {
		used[id] = true
	}
	for _, id := range h.VoteManager.GetSessionIDs() {
		used[id] = true
	}

	var walk func(prefix []byte) string
	walk = func(prefix []byte) string {
		if len(prefix) == codeLen {
			s := string(prefix)
			if !used[s] {
				h.Connections[s] = &SessionConnections{Stagiaires: make(map[string]*Client)}
				return s
			}
			return ""
		}
		for _, c := range alphabet {
			next := append(append([]byte{}, prefix...), c)
			if found := walk(next); found != "" {
				return found
			}
		}
		return ""
	}

	return walk(nil)
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

func (h *Hub) registerClient(client *Client) {
	var pending []pendingSend

	// Deferred flush runs AFTER the unlock defer (LIFO): all sends
	// happen outside the hub write-lock (CC3).
	defer func() {
		for _, p := range pending {
			p.flush()
		}
	}()

	h.mu.Lock()
	defer h.mu.Unlock()

	queue := func(c *Client, msg map[string]any) {
		pending = append(pending, pendingSend{c, msg})
	}
	queueErr := func(c *Client, message string) {
		queue(c, map[string]any{"type": "error", "message": message})
	}

	conns, exists := h.Connections[client.SessionID]

	// S13: any join to an already-minted Manager session requires the
	// per-session trainer token — including the empty-slot recovery
	// path (conns.Trainer == nil after the previous trainer's readPump
	// exited). The previous code gated the token check on `old != nil`,
	// so once the legitimate trainer's readPump exited and
	// unregisterClient set conns.Trainer = nil, any client who knew the
	// public 3-char session code (printed in every stagiaire's QR)
	// could claim the slot AND receive the minted token in
	// session_created — collapsing S1's takeover protection for the
	// rest of the session. Worse, the token is minted once and never
	// rotated, so the attacker held it for as long as the session
	// lived.
	//
	// The check runs BEFORE the !exists branch so a tokenless claimant
	// cannot bypass it by triggering the createSessionFn path (which
	// would otherwise overwrite the existing session's minted token).
	// The legitimate trainer always has the token (sessionStorage,
	// scoped to the tab, populated from the session_created payload at
	// creation time and re-confirmed on every authenticated reconnect);
	// an unauthenticated claimant is rejected with the same "Session
	// introuvable" message the no-session branch uses so the response
	// does not leak that the code is live.
	//
	// Fresh session creation (no Manager session at this point) is the
	// only path that mints and emits the token to a previously-
	// unauthenticated trainer — the creator IS the legitimate trainer.
	if client.Type == "trainer" {
		if sess, ok := h.VoteManager.GetSession(client.SessionID); ok && sess.GetTrainerToken() != "" {
			if !h.VoteManager.ValidateTrainerToken(client.SessionID, client.TrainerToken) {
				queueErr(client, "Session introuvable")
				return
			}
		}
	}

	if !exists {
		if client.Type == "trainer" {
			conns = &SessionConnections{
				Stagiaires: make(map[string]*Client),
			}
			h.Connections[client.SessionID] = conns
			// S13 defensive guard: if a Manager session already exists
			// here (Connections entry was reaped while the Manager
			// session survived — not reachable via cleanupLoop's normal
			// contract, but defense in depth), do NOT call
			// createSessionFn. It would overwrite the existing minted
			// token, minting a fresh one for the claimant and defeating
			// the S13 check above. The trainer was authenticated above;
			// the existing session is theirs to recover.
			if _, ok := h.VoteManager.GetSession(client.SessionID); !ok {
				if _, err := createSessionFn(h.VoteManager, client.SessionID, client.ID); err != nil {
					slog.Error("Failed to create session",
						append([]any{"error", err}, client.logAttrs()...)...)
					delete(h.Connections, client.SessionID)
					queueErr(client, "Impossible de créer la session")
					return
				}
			}
		} else {
			queueErr(client, "Session introuvable")
			return
		}
	}

	if client.Type == "trainer" {
		if old := conns.Trainer; old != nil && old != client {
			// Active takeover. The token was validated above; this branch
			// just closes the outgoing trainer's connection. S1's
			// per-session token gate is what made takeover require
			// proof of ownership; S13 extended it to the recovery path.
			queueErr(old, "New trainer connection detected, closing this one.")
			// CL1: flag the outgoing trainer so any in-flight broadcast
			// captured before the swap drops silently instead of pushing
			// onto the stale Send channel.
			old.markClosing()
			time.AfterFunc(trainerTakeoverCloseDelay, func() {
				if old.Conn != nil {
					old.Conn.Close()
				}
			})
		}

		conns.Trainer = client
		if _, ok := h.VoteManager.GetSession(client.SessionID); !ok {
			if _, err := createSessionFn(h.VoteManager, client.SessionID, client.ID); err != nil {
				slog.Error("Failed to create session",
					append([]any{"error", err}, client.logAttrs()...)...)
				conns.Trainer = nil
				queueErr(client, "Impossible de créer la session")
				return
			}
		} else if err := updateTrainerFn(h.VoteManager, client.SessionID, client.ID); err != nil {
			// R5: UpdateTrainer can return ErrSessionNotFound when the
			// session is reaped between the GetSession check above and
			// the UpdateTrainer call (cleanupLoop, concurrent reaper,
			// etc.). Without recovery, conns.Trainer stays set with no
			// Manager session underneath: every later op returns
			// ErrSessionNotFound, and cleanupLoop can't reap the entry
			// because conns.Trainer != nil. Fall back to CreateSession
			// (the !ok branch's path) so the trainer keeps a working
			// session; if CreateSession also fails, roll conns.Trainer
			// back so cleanupLoop can reap.
			slog.Warn("UpdateTrainer failed; falling back to CreateSession",
				append([]any{"error", err}, client.logAttrs()...)...)
			if _, cerr := createSessionFn(h.VoteManager, client.SessionID, client.ID); cerr != nil {
				slog.Error("Failed to create session after UpdateTrainer fallback",
					append([]any{"error", cerr}, client.logAttrs()...)...)
				conns.Trainer = nil
				queueErr(client, "Impossible de créer la session")
				return
			}
		}

		sessionCreated := map[string]any{
			"type":        "session_created",
			"sessionCode": client.SessionID,
			"trainerId":   client.ID,
		}
		// Emit the token to the trainer. With S13 every trainer reaching
		// this point is either the creator of a freshly-minted session
		// or has already validated the per-session token above — never an
		// unauthenticated claimant. The client persists it (sessionStorage,
		// scoped to the tab) and re-sends on every trainer_join so
		// reconnects can re-validate against the same gate.
		if sess, ok := h.VoteManager.GetSession(client.SessionID); ok {
			if token := sess.GetTrainerToken(); token != "" {
				sessionCreated["trainerToken"] = token
			}
		}
		queue(client, sessionCreated)

		if ps := h.buildTrainerStagiaireListLocked(conns, client.SessionID, "connected_count"); ps != nil {
			pending = append(pending, *ps)
		}

		if session, ok := h.VoteManager.GetSession(client.SessionID); ok {
			state, colors, multipleChoice, voteStartTime := session.GetState()
			labels := session.GetActiveLabels()
			gameEnabled := session.GetGameEnabled()
			competitive := session.GetCompetitive()
			allowBlank := session.GetAllowBlank()

			if state == models.VoteStateActive || state == models.VoteStateClosed {
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
				queue(client, replayMsg)

				votes := session.GetVotes()
				stagiaires := session.GetStagiaires()
				for sID, vColors := range votes {
					sName := stagiaires[sID]
					queue(client, map[string]any{
						"type":          "vote_received",
						"stagiaireId":   sID,
						"stagiaireName": sName,
						"colors":        vColors,
					})
				}

				if state == models.VoteStateClosed {
					queue(client, map[string]any{"type": "vote_closed"})
				}

				if competitive && session.GetRevealed() {
					scores := session.GetScores()
					gameScores := session.GetGameScores()
					correctColors := session.GetCorrectColors()
					queue(client, map[string]any{
						"type":          "answers_revealed",
						"correctColors": correctColors,
						"scores":        buildScoreboard(stagiaires, votes, scores, gameScores),
					})
				}
			} else if len(colors) > 0 {
				// Only sync config when the session has been configured (a
				// previous trainer picked colors). On a fresh session we have
				// nothing useful to send — empty colors would clobber the
				// client's autoloaded last-config.
				//
				// FH4: include the full config surface (labels, feature
				// flags) so a reconnecting trainer's view matches the
				// server's canonical state rather than their localStorage
				// snapshot. The frontend's session_created handler runs
				// applyLastConfigIfAvailable first; this message then
				// overrides with whatever the server actually has stored,
				// which is the source of truth across devices/trainers.
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
				queue(client, replayMsg)
			}
		}
	} else {
		if conns.Trainer == nil {
			queueErr(client, "Session indisponible (aucun formateur connecté)")
			return
		}

		// R16: a *Client that re-joins under a different stagiaireId
		// would otherwise leave its prior slot in conns.Stagiaires
		// pointing at the same pointer forever. Client.ID is mutable
		// across stagiaire_join messages on one connection (R1 resets
		// it to OriginalID, then a presented-and-known stagiaireId
		// overwrites it); registerClient only cleaned the *current* ID
		// slot and unregisterClient only deletes the *current* ID. So a
		// client registering as B after joining as A left
		// conns.Stagiaires["A"] → same *Client — inflating
		// connected_count (a phantom the trainer can never clear) and
		// leaking a MaxClientsPerSession slot per cycle. A malicious
		// client cycling stolen (id, token) pairs could exhaust the cap
		// and block legitimate joins with "Session complète". The scan
		// runs BEFORE the cap check so the count reflects reality, and
		// it is O(N) per join where N is bounded by the cap (the common
		// case has zero matches).
		for id, c := range conns.Stagiaires {
			if c == client && id != client.ID {
				delete(conns.Stagiaires, id)
			}
		}

		// S7: per-session client cap. Counts the trainer slot too so the
		// effective stagiaire limit is cap-1; the trainer is always
		// allowed to reclaim their own session regardless. A zero or
		// negative config disables the cap (used by tests that don't
		// care about the bound).
		if h.Config.MaxClientsPerSession > 0 && len(conns.Stagiaires)+1 >= h.Config.MaxClientsPerSession {
			queueErr(client, "Session complète — réessayez plus tard")
			return
		}

		result, err := h.VoteManager.JoinStagiaire(client.SessionID, client.ID, client.Name, client.ReclaimToken)
		if err != nil {
			// CC2: JoinStagiaire is the authoritative name-uniqueness
			// arbiter (under the session lock). The advisory check in
			// handleStagiaireJoin handles the common case; this catches
			// the TOCTOU race. S6/S12: it is also the sole authority
			// for reclaim-token validation. S15: it also authoritatively
			// rejects reclaim-rename collisions. B3: every manager error
			// routes through UserFacingError so the wire stays French
			// and no internal entity name leaks into a classroom toast.
			queueErr(client, vote.UserFacingError(err))
			return
		}

		// CL1: flag an outgoing client that held this same ID (a
		// different connection reconnecting by ID) so any in-flight
		// broadcast captured before the swap drops silently instead of
		// pushing onto the stale Send channel. R16: skip when the slot
		// already belongs to this same *Client (a same-ID re-register is
		// a no-op reconnect and must not close the client's own conn).
		if old, ok := conns.Stagiaires[client.ID]; ok && old != client {
			old.markClosing()
			if old.Conn != nil {
				old.Conn.Close()
			}
		}
		conns.Stagiaires[client.ID] = client

		// S6/S12: surface the reclaim token so the client can prove
		// ownership of this identity on future reconnects. Without the
		// token, a stale stagiaireId alone is not enough to inherit the
		// prior Scores/GameScores (which are the actual high-value
		// targets — they're what the competitive leaderboard ranks on).
		joined := map[string]any{
			"type":         "session_joined",
			"sessionCode":  client.SessionID,
			"stagiaireId":  client.ID,
			"reclaimToken": result.ReclaimToken,
		}
		queue(client, joined)

		if ps := h.buildTrainerStagiaireListLocked(conns, client.SessionID, "connected_count"); ps != nil {
			pending = append(pending, *ps)
		}

		session, ok := h.VoteManager.GetSession(client.SessionID)
		if ok {
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
				queue(client, msg)
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
				queue(client, msg)
				queue(client, map[string]any{"type": "vote_closed"})
				if competitive && session.GetRevealed() {
					correctColors := session.GetCorrectColors()
					scores := session.GetScores()
					gameScores := session.GetGameScores()
					rank, total := computeRank(scores, gameScores, client.ID)
					queue(client, map[string]any{
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
			h.mu.RLock()
			protected := make(map[string]bool)
			for id, conns := range h.Connections {
				if conns.Trainer != nil || len(conns.Stagiaires) > 0 {
					protected[id] = true
				}
			}
			h.mu.RUnlock()

			h.VoteManager.CleanupExpiredSessions(h.Config.SessionTimeout, protected)

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

func computeRank(voteScores, gameScores map[string]int, id string) (int, int) {
	total := len(voteScores)
	if total == 0 {
		return 0, 0
	}
	myScore := voteScores[id] + gameScores[id]
	rank := 1
	for otherID := range voteScores {
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

func (h *Hub) GetConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.Connections)
}

// ProductStats returns the aggregate usage counters (sessions, votes,
// trainees, feature adoption) collected by the vote Manager. Exposed via the
// /metrics endpoint for maintainer insights.
func (h *Hub) ProductStats() vote.ProductStatsSnapshot {
	return h.VoteManager.Stats().Snapshot()
}
