package hub

import (
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/models"
	"vote-backend/internal/vote"
)

const (
	// ClientSendBufferSize bounds the per-client outbound queue.
	// Raised from 256 to 512 so a 30-stagiaire burst (≈60 trainer-bound
	// messages: vote_received + connected_count per vote) fits with
	// headroom even if the trainer's writePump briefly stalls (CC1).
	ClientSendBufferSize = 512
	pongWait             = 70 * time.Second
	MaxGameScore         = 100000
)

type Client struct {
	ID           string
	Name         string
	SessionID    string
	Type         string
	TrainerToken string
	// ReclaimToken is the per-stagiaire secret presented on reconnect
	// to prove ownership of an existing identity (S6/S12). Captured
	// from the stagiaire_join message and forwarded to
	// VoteManager.JoinStagiaire, which is the sole authority for the
	// constant-time compare against session.ReclaimTokens[id].
	ReclaimToken string
	Conn         *websocket.Conn
	Send         chan []byte
	Hub          *Hub
	pingTick     *time.Ticker
	IP           string
	handlers     map[string]func(models.Message)
	// closing is set when the connection is being torn down
	// (slow-buffer eviction, reconnect-by-ID takeover, trainer
	// takeover). Once set, SendJSON short-circuits so stale
	// references — e.g. a BroadcastSession target snapshot captured
	// just before the eviction — drop silently instead of piling
	// messages onto a dead channel and spamming the log on every
	// subsequent broadcast until pongWait evicts the entry (CL1, CM3).
	closing atomic.Bool
}

func NewClient(hub *Hub, conn *websocket.Conn, ip string) *Client {
	c := &Client{
		Hub:      hub,
		Conn:     conn,
		Send:     make(chan []byte, ClientSendBufferSize),
		pingTick: time.NewTicker(hub.Config.PingInterval),
		IP:       ip,
	}

	c.handlers = map[string]func(models.Message){
		"trainer_join":      c.handleTrainerJoin,
		"stagiaire_join":    c.handleStagiaireJoin,
		"start_vote":        c.handleStartVote,
		"vote":              c.handleVote,
		"close_vote":        c.handleCloseVote,
		"reset_vote":        c.handleResetVote,
		"reveal_answers":    c.handleRevealAnswers,
		"report_game_score": c.handleReportGameScore,
		"update_name":       c.handleUpdateName,
	}

	return c
}

func (c *Client) Start() {
	// CM1: register both pumps with the Hub's WaitGroup so Shutdown can
	// wait for them to fully exit. Without this, Shutdown returns while
	// writePumps are still mid-writeMessage → truncated frames on the
	// wire, and the process can exit with goroutines touching the conn.
	c.Hub.wg.Add(2)
	go c.readPump()
	go c.writePump()
}

func (c *Client) readPump() {
	defer c.Hub.wg.Done()
	defer func() {
		c.Hub.Security.RemoveMessageRate(c.ID)
		select {
		case c.Hub.Unregister <- c:
		case <-c.Hub.Context().Done():
		}
		c.Conn.Close()
		if c.pingTick != nil {
			c.pingTick.Stop()
		}
	}()

	c.Conn.SetReadLimit(4096)
	if err := c.Conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		slog.Error("Failed to set read deadline", "error", err)
		return
	}
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Error("WebSocket error", "error", err)
			}
			break
		}

		if !c.Hub.Security.CheckMessageRate(c.ID) {
			slog.Warn("Rate limit exceeded", "client_id", c.ID)
			c.SendError("Trop de messages, veuillez ralentir")
			continue
		}

		c.handleMessage(message)
	}
}

func (c *Client) writePump() {
	defer c.Hub.wg.Done()
	defer func() {
		if c.pingTick != nil {
			c.pingTick.Stop()
		}
		_ = c.Conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(c.Hub.Config.WriteTimeout))
			if !ok {
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Error("Write error", "error", err)
				return
			}
		case <-c.pingTick.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(c.Hub.Config.WriteTimeout))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.Hub.Context().Done():
			// CM1: shutdown — stop writing. readPump's defer will close
			// the conn once Shutdown calls Conn.Close.
			return
		}
	}
}

func (c *Client) handleMessage(data []byte) {
	var msg models.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Error("JSON unmarshal error", "error", err)
		return
	}

	if handler, ok := c.handlers[msg.Type]; ok {
		handler(msg)
	} else {
		slog.Warn("Unknown message type", "type", msg.Type)
	}
}

// markClosing flags the client as being torn down. Future SendJSON /
// trySend calls become no-ops, so stale references captured before the
// eviction (e.g. a BroadcastSession target list) drop silently instead
// of pushing into a dead channel (CL1) or logging a warning on every
// subsequent broadcast until pongWait reaps the entry (CM3).
func (c *Client) markClosing() {
	c.closing.Store(true)
}

func (c *Client) SendJSON(v any) {
	if c.closing.Load() {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("Marshal error", "error", err)
		return
	}
	c.trySend(data)
}

// trySend enqueues a pre-marshalled payload with buffer-full semantics
// that depend on the client role (CC1). A stagiaire whose queue is full
// is evicted — they will reconnect and the protocol is idempotent. The
// trainer is never evicted for a transient burst: disconnecting the
// trainer disrupts the entire class, so the message is dropped instead
// and the next connected_count reconciles the view.
func (c *Client) trySend(data []byte) {
	if c.closing.Load() {
		return
	}
	select {
	case c.Send <- data:
	default:
		if c.Type == "trainer" {
			slog.Warn("Trainer send buffer full: dropping message", "client_id", c.ID)
		} else {
			slog.Warn("Send channel full: disconnecting slow client", "client_id", c.ID)
			c.markClosing()
			if c.Conn != nil {
				c.Conn.Close()
			}
		}
	}
}

func (c *Client) SendError(message string) {
	c.SendJSON(map[string]any{
		"type":    "error",
		"message": message,
	})
}

// Handlers that interface with VoteManager and Hub

func (c *Client) handleTrainerJoin(msg models.Message) {
	var code string

	// If no code provided or "new" is specified, generate a unique code
	if msg.SessionCode == "" || msg.SessionCode == "new" {
		// Per-IP session-creation cap. Prevents one client from exhausting
		// the code space or server memory by spamming new sessions. Generous
		// enough that multiple trainers behind the same NAT (a building
		// with several classrooms) never hit it.
		if !c.Hub.Security.CheckSessionCreateRate(c.IP) {
			c.SendError("Trop de sessions créées — réessayez dans quelques minutes")
			return
		}
		// S7: global session cap. Bounds memory growth across the whole
		// process — a runaway trainer script (or a misconfigured load
		// balancer retrying /ws) can't OOM the server. The cap counts
		// Connections entries, which includes any reserved-but-not-yet-
		// registered codes from concurrent trainers.
		if c.Hub.AtSessionsCap() {
			c.SendError("Trop de sessions actives — réessayez plus tard")
			return
		}
		code = c.Hub.GenerateSessionCode()
		if code == "" {
			c.SendError("No session codes available")
			return
		}
		// Optimistically record; if registration later fails we'll roll back.
		c.Hub.Security.RecordSessionCreation(c.IP)
	} else {
		// Validate provided code
		if !vote.IsValidSessionCode(msg.SessionCode) {
			c.Hub.Security.RecordFailedJoin(c.IP)
			c.SendJSON(map[string]any{"type": "error", "message": "Code session invalide"})
			return
		}

		// Trainer can only join existing sessions with specific codes
		// To create a new session, use "new" or empty code
		if !c.Hub.SessionExists(msg.SessionCode) && !c.Hub.VoteManager.SessionExists(msg.SessionCode) {
			// Record the failure so the per-IP exponential backoff applies to
			// session-code enumeration via trainer_join too. Without this,
			// all 12,167 codes are enumerable in ~20 min (S2).
			c.Hub.Security.RecordFailedJoin(c.IP)
			c.SendJSON(map[string]any{"type": "error", "message": "Session introuvable"})
			return
		}
		code = msg.SessionCode
	}

	// Capture the presented token; registerClient validates it under the hub
	// lock before allowing takeover of an active trainer (S1).
	c.TrainerToken = msg.TrainerToken
	c.Type = "trainer"
	c.SessionID = code
	c.Hub.Security.ClearFailedJoin(c.IP)

	select {
	case c.Hub.Register <- c:
	case <-c.Hub.Context().Done():
		// Roll back the creation counter so the failed attempt doesn't
		// consume the trainer's quota.
		if msg.SessionCode == "" || msg.SessionCode == "new" {
			c.Hub.Security.RemoveSessionCreation(c.IP)
		}
		c.SendError("Server is shutting down")
	}
}

func (c *Client) handleStagiaireJoin(msg models.Message) {
	if !vote.IsValidSessionCode(msg.SessionCode) {
		c.SendErrorWithBackoff("Code session invalide")
		return
	}
	if msg.Name != "" && !vote.IsValidName(msg.Name) {
		c.SendErrorWithBackoff("Nom invalide")
		return
	}

	c.Type = "stagiaire"
	c.Name = msg.Name
	c.SessionID = msg.SessionCode
	c.ReclaimToken = msg.ReclaimToken

	// Check if session exists via Hub (which checks Manager/Connections)
	if !c.Hub.SessionExists(c.SessionID) {
		c.SendErrorWithBackoff("Session introuvable")
		return
	}

	// Identity resolution. The authoritative reclaim-token check runs
	// inside JoinStagiaire (under the session lock, constant-time
	// compare); the lookups here are advisory fast-fails so the common
	// "stale ID after a server restart" case doesn't round-trip
	// through Hub.Run just to be rejected.
	//
	// S6: name-based reclaim is gone. Two clients presenting the same
	// name no longer collapse onto one identity (and its scores) —
	// they're distinct stagiaires, and the second one is rejected with
	// ErrNameInUse. The only way to attach to an existing identity is
	// to present its ID AND its reclaim token.
	if msg.StagiaireID != "" && vote.IsValidStagiaireID(msg.StagiaireID) {
		if session, ok := c.Hub.VoteManager.GetSession(c.SessionID); ok {
			stagiaires := session.GetStagiaires()
			if _, exists := stagiaires[msg.StagiaireID]; exists {
				// ID is known: keep c.ID so JoinStagiaire takes the
				// reclaim path. The token is checked authoritatively
				// under the session lock; this advisory lookup just
				// confirms the ID is plausibly the client's.
				c.ID = msg.StagiaireID

				// Fast-fail the rename-collision case before round-
				// tripping through Hub.Run, so a typo doesn't consume
				// the user's backoff budget.
				if msg.Name != "" {
					if existingName := stagiaires[msg.StagiaireID]; vote.NormalizeName(msg.Name) != vote.NormalizeName(existingName) {
						if c.Hub.VoteManager.IsNameInUse(c.SessionID, msg.Name, c.ID) {
							c.SendErrorWithBackoff("Ce nom est déjà utilisé")
							return
						}
					}
				}
			}
			// else: presented ID is not in the session. Leave c.ID as
			// the server-generated value; JoinStagiaire treats it as a
			// fresh join and mints a new reclaim token. The stale ID
			// on the client will be overwritten by session_joined.
		}
	}

	c.Hub.Security.ClearFailedJoin(c.IP)

	select {
	case c.Hub.Register <- c:
	case <-c.Hub.Context().Done():
		c.SendError("Server is shutting down")
	}
}

func (c *Client) SendErrorWithBackoff(msg string) {
	c.Hub.Security.RecordFailedJoin(c.IP)
	c.SendJSON(map[string]any{
		"type":    "error",
		"message": msg,
	})
}

func (c *Client) handleStartVote(msg models.Message) {
	if c.Type != "trainer" {
		c.SendError(vote.ErrNotAuthorized.Error())
		return
	}
	// Validate colors
	if len(msg.Colors) == 0 {
		c.SendError("Au moins une couleur est requise")
		return
	}
	if !vote.ValidateColors(msg.Colors, c.Hub.Config.ValidColors) {
		c.SendError("Couleur(s) invalide(s)")
		return
	}
	// Check for duplicates
	if vote.HasDuplicates(msg.Colors) {
		c.SendError(vote.ErrDuplicateColors.Error())
		return
	}

	// Validate labels if provided. BM5: labels must reference colors in
	// the selected palette (msg.Colors), not the global ValidColors —
	// otherwise a trainer could attach a label to a color that isn't
	// even on the ballot.
	if len(msg.Labels) > 0 {
		if !vote.ValidateLabels(msg.Labels, msg.Colors) {
			c.SendError("Étiquettes invalides")
			return
		}
	}
	err := c.Hub.VoteManager.StartVote(c.SessionID, c.ID, msg.Colors, msg.MultipleChoice, msg.Labels, msg.GameEnabled, msg.Competitive, msg.AllowBlank)

	if err != nil {
		c.SendError(vote.UserFacingError(err))
		return
	}

	var voteStartTime int64
	if session, ok := c.Hub.VoteManager.GetSession(c.SessionID); ok {
		_, _, _, voteStartTime = session.GetState()
	}

	broadcastMsg := map[string]any{
		"type":           "vote_started",
		"colors":         msg.Colors,
		"multipleChoice": msg.MultipleChoice,
		"voteStartTime":  voteStartTime,
		"voteElapsed":    time.Now().Unix() - voteStartTime,
		"gameEnabled":    msg.GameEnabled,
		"competitive":    msg.Competitive,
		"allowBlank":     msg.AllowBlank,
	}
	if len(msg.Labels) > 0 {
		broadcastMsg["labels"] = msg.Labels
	}

	c.Hub.BroadcastSession(c.SessionID, broadcastMsg, "")

	// Send updated stagiaire list (votes are now cleared)
	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleVote(msg models.Message) {
	if c.Type != "stagiaire" {
		c.SendError(vote.ErrNotAuthorized.Error())
		return
	}
	if vote.HasDuplicates(msg.Colors) {
		c.SendError(vote.ErrDuplicateColors.Error())
		return
	}
	stagiaireName, err := c.Hub.VoteManager.SubmitVote(c.SessionID, c.ID, msg.Colors)
	if err != nil {
		c.SendError(vote.UserFacingError(err))
		return
	}

	if stagiaireName == "" {
		stagiaireName = c.Name
	}

	c.SendJSON(map[string]any{"type": "vote_accepted"})

	// Notify trainer
	c.Hub.SendToTrainer(c.SessionID, map[string]any{
		"type":          "vote_received",
		"stagiaireId":   c.ID,
		"stagiaireName": stagiaireName,
		"colors":        msg.Colors,
	})

	// Also send updated stagiaire list with vote status
	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleCloseVote(_ models.Message) {
	if c.Type != "trainer" {
		c.SendError(vote.ErrNotAuthorized.Error())
		return
	}
	err := c.Hub.VoteManager.CloseVote(c.SessionID, c.ID)
	if err != nil {
		c.SendError(vote.UserFacingError(err))
		return
	}
	c.Hub.BroadcastSession(c.SessionID, map[string]any{"type": "vote_closed"}, "")
}

func (c *Client) handleResetVote(msg models.Message) {
	if c.Type != "trainer" {
		c.SendError(vote.ErrNotAuthorized.Error())
		return
	}
	// Validate colors if provided
	if len(msg.Colors) > 0 {
		if !vote.ValidateColors(msg.Colors, c.Hub.Config.ValidColors) {
			c.SendError("Couleur(s) invalide(s)")
			return
		}
		if vote.HasDuplicates(msg.Colors) {
			c.SendError(vote.ErrDuplicateColors.Error())
			return
		}
	}
	// Validate labels if provided. BM5: labels must reference colors in
	// the selected palette (msg.Colors when provided, otherwise the
	// session's existing ActiveColors). On a reset_vote with no colors
	// supplied there is no palette to label against, so we accept any
	// labels — ResetVote clears ActiveLabels unconditionally.
	if len(msg.Labels) > 0 && len(msg.Colors) > 0 {
		if !vote.ValidateLabels(msg.Labels, msg.Colors) {
			c.SendError("Étiquettes invalides")
			return
		}
	}
	err := c.Hub.VoteManager.ResetVote(c.SessionID, c.ID, msg.Colors, msg.MultipleChoice, msg.Labels, msg.GameEnabled, msg.Competitive, msg.AllowBlank)
	if err != nil {
		c.SendError(vote.UserFacingError(err))
		return
	}
	c.Hub.BroadcastSession(c.SessionID, map[string]any{
		"type":        "vote_reset",
		"gameEnabled": msg.GameEnabled,
		"competitive": msg.Competitive,
		"allowBlank":  msg.AllowBlank,
	}, "")

	// Send updated stagiaire list (votes are now cleared)
	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleRevealAnswers(msg models.Message) {
	if c.Type != "trainer" {
		c.SendError(vote.ErrNotAuthorized.Error())
		return
	}

	session, ok := c.Hub.VoteManager.GetSession(c.SessionID)
	if !ok {
		c.SendError("Session introuvable")
		return
	}
	activeColors := session.GetActiveColorsRaw()

	for _, color := range msg.CorrectColors {
		if color == "blank" {
			if !session.GetAllowBlank() {
				c.SendError(vote.ErrBlankNotAllowed.Error())
				return
			}
			continue
		}
		if !vote.ValidateColors([]string{color}, c.Hub.Config.ValidColors) {
			c.SendError("Couleur(s) invalide(s)")
			return
		}
		found := false
		for _, ac := range activeColors {
			if ac == color {
				found = true
				break
			}
		}
		if !found {
			c.SendError("Couleur absente de la palette active")
			return
		}
	}

	entries, err := c.Hub.VoteManager.RevealAnswers(c.SessionID, c.ID, msg.CorrectColors)
	if err != nil {
		c.SendError(vote.UserFacingError(err))
		return
	}

	correctColors := session.GetCorrectColors()

	c.Hub.SendToTrainer(c.SessionID, map[string]any{
		"type":          "answers_revealed",
		"correctColors": correctColors,
		"scores":        entries,
	})

	c.Hub.SendScoreReveal(c.SessionID, correctColors, entries)

	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleReportGameScore(msg models.Message) {
	if c.Type != "stagiaire" {
		return
	}
	if msg.GameScore < 0 || msg.GameScore > MaxGameScore {
		return
	}
	session, ok := c.Hub.VoteManager.GetSession(c.SessionID)
	if !ok {
		return
	}
	if !session.GetGameEnabled() {
		return
	}
	err := c.Hub.VoteManager.UpdateGameScore(c.SessionID, c.ID, msg.GameScore)
	if err != nil {
		return
	}
	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleUpdateName(msg models.Message) {
	err := c.Hub.VoteManager.UpdateStagiaireName(c.SessionID, c.ID, msg.Name)
	if err != nil {
		c.SendError(vote.UserFacingError(err))
		return
	}
	c.Name = msg.Name
	c.SendJSON(map[string]any{"type": "name_updated", "name": msg.Name})

	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "stagiaire_names_updated")
}
