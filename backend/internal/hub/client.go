package hub

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/models"
	"vote-backend/internal/vote"
)

const (
	ClientSendBufferSize = 256
	pongWait             = 70 * time.Second
)

type Client struct {
	ID           string
	Name         string
	SessionID    string
	Type         string
	Conn         *websocket.Conn
	Send         chan []byte
	Hub          *Hub
	pingTick     *time.Ticker
	IP           string
	LastActivity int64
	handlers     map[string]func(models.Message)
}

func NewClient(hub *Hub, conn *websocket.Conn, ip string) *Client {
	c := &Client{
		Hub:          hub,
		Conn:         conn,
		Send:         make(chan []byte, ClientSendBufferSize),
		pingTick:     time.NewTicker(hub.Config.PingInterval),
		IP:           ip,
		LastActivity: time.Now().Unix(),
	}

	c.handlers = map[string]func(models.Message){
		"trainer_join":   c.handleTrainerJoin,
		"stagiaire_join": c.handleStagiaireJoin,
		"start_vote":     c.handleStartVote,
		"vote":           c.handleVote,
		"close_vote":     c.handleCloseVote,
		"reset_vote":     c.handleResetVote,
		"update_name":    c.handleUpdateName,
	}

	return c
}

func (c *Client) Start() {
	go c.readPump()
	go c.writePump()
}

func (c *Client) readPump() {
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
		c.LastActivity = time.Now().Unix()
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

func (c *Client) SendJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("Marshal error", "error", err)
		return
	}
	select {
	case c.Send <- data:
	default:
		slog.Warn("Send channel full", "client_id", c.ID)
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
		code = c.Hub.GenerateSessionCode()
		if code == "" {
			c.SendError("No session codes available")
			return
		}
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
			c.SendJSON(map[string]any{"type": "error", "message": "Session introuvable"})
			return
		}
		code = msg.SessionCode
	}

	c.Type = "trainer"
	c.SessionID = code
	c.Hub.Security.ClearFailedJoin(c.IP)

	select {
	case c.Hub.Register <- c:
	case <-c.Hub.Context().Done():
		c.SendError("Server is shutting down")
	}
}

func (c *Client) handleStagiaireJoin(msg models.Message) {
	if !vote.IsValidSessionCode(msg.SessionCode) {
		c.SendErrorWithBackoff("Invalid session code")
		return
	}
	if msg.Name != "" && !vote.IsValidName(msg.Name) {
		c.SendErrorWithBackoff("Invalid name")
		return
	}

	c.Type = "stagiaire"
	c.Name = msg.Name
	c.SessionID = msg.SessionCode

	// Check if session exists via Hub (which checks Manager/Connections)
	if !c.Hub.SessionExists(c.SessionID) {
		c.SendErrorWithBackoff("Session introuvable")
		return
	}

	// Identity Management Logic
	finalID := c.ID // Start with the random ID
	foundExisting := false
	isCredentialed := false

	// 1. Try reclaim by ID (highest priority - exact match - PROOF OF OWNERSHIP)
	var existingName string
	if msg.StagiaireID != "" {
		if session, ok := c.Hub.VoteManager.GetSession(c.SessionID); ok {
			if name, exists := session.GetStagiaires()[msg.StagiaireID]; exists {
				finalID = msg.StagiaireID
				foundExisting = true
				isCredentialed = true
				existingName = name
			}
		}
	}

	// 1.5. When reconnecting with credentials, validate the name
	// If the provided name differs from the stored name (case-insensitive), check for collision
	if isCredentialed && msg.Name != "" && vote.NormalizeName(msg.Name) != vote.NormalizeName(existingName) {
		if c.Hub.VoteManager.IsNameInUse(c.SessionID, msg.Name, finalID) {
			c.SendErrorWithBackoff("Ce nom est déjà utilisé")
			return
		}
	}

	// 2. If not found by ID, try match by Name (Claiming identity)
	if !foundExisting {
		if existingID, found := c.Hub.VoteManager.GetStagiaireIDByName(c.SessionID, msg.Name); found {
			finalID = existingID
			foundExisting = true
			isCredentialed = false
		}
	}

	// 3. Collision Check
	// If we matched by NAME only (no credential), and the user is online, we BLOCK.
	// If we matched by ID (credential), we ALLOW (this is a reconnect/takeover).
	if foundExisting && !isCredentialed && c.Hub.IsStagiaireConnected(c.SessionID, finalID) {
		c.SendErrorWithBackoff("Ce nom est déjà utilisé par une personne connectée")
		return
	}

	// 4. Apply Identity
	c.ID = finalID
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
	// Validate colors
	if len(msg.Colors) == 0 {
		c.SendError("At least one color is required")
		return
	}
	if !vote.ValidateColors(msg.Colors, c.Hub.Config.ValidColors) {
		c.SendError("Invalid color(s)")
		return
	}
	// Check for duplicates
	if vote.HasDuplicates(msg.Colors) {
		c.SendError("Duplicate colors are not allowed")
		return
	}

	// Validate labels if provided
	if len(msg.Labels) > 0 {
		if !vote.ValidateLabels(msg.Labels, c.Hub.Config.ValidColors) {
			c.SendError("Invalid labels")
			return
		}
	}
	err := c.Hub.VoteManager.StartVote(c.SessionID, c.ID, msg.Colors, msg.MultipleChoice, msg.Labels)

	if err != nil {
		c.SendError(err.Error())
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
	}
	if len(msg.Labels) > 0 {
		broadcastMsg["labels"] = msg.Labels
	}

	c.Hub.BroadcastSession(c.SessionID, broadcastMsg, "")

	// Send updated stagiaire list (votes are now cleared)
	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleVote(msg models.Message) {
	if vote.HasDuplicates(msg.Colors) {
		c.SendError("Duplicate colors are not allowed")
		return
	}
	stagiaireName, err := c.Hub.VoteManager.SubmitVote(c.SessionID, c.ID, msg.Colors)
	if err != nil {
		c.SendError(err.Error())
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
	err := c.Hub.VoteManager.CloseVote(c.SessionID, c.ID)
	if err != nil {
		c.SendError(err.Error())
		return
	}
	c.Hub.BroadcastSession(c.SessionID, map[string]any{"type": "vote_closed"}, "")
}

func (c *Client) handleResetVote(msg models.Message) {
	// Validate colors if provided
	if len(msg.Colors) > 0 {
		if !vote.ValidateColors(msg.Colors, c.Hub.Config.ValidColors) {
			c.SendError("Invalid color(s)")
			return
		}
		if vote.HasDuplicates(msg.Colors) {
			c.SendError("Duplicate colors are not allowed")
			return
		}
	}
	err := c.Hub.VoteManager.ResetVote(c.SessionID, c.ID, msg.Colors, msg.MultipleChoice, nil)
	if err != nil {
		c.SendError(err.Error())
		return
	}
	c.Hub.BroadcastSession(c.SessionID, map[string]any{"type": "vote_reset"}, "")

	// Send updated stagiaire list (votes are now cleared)
	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "connected_count")
}

func (c *Client) handleUpdateName(msg models.Message) {
	err := c.Hub.VoteManager.UpdateStagiaireName(c.SessionID, c.ID, msg.Name)
	if err != nil {
		c.SendError(err.Error())
		return
	}
	c.Name = msg.Name
	c.SendJSON(map[string]any{"type": "name_updated", "name": msg.Name})

	c.Hub.NotifyTrainerStagiaireList(c.SessionID, "stagiaire_names_updated")
}
