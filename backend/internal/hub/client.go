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
)

type Client struct {
	ID           string
	ConnID       int64
	Name         string
	SessionID    string
	Type         string
	Conn         *websocket.Conn
	Send         chan []byte
	Hub          *Hub
	pingTick     *time.Ticker
	IP           string
	LastActivity int64
}

func NewClient(hub *Hub, conn *websocket.Conn, ip string) *Client {
	return &Client{
		Hub:          hub,
		Conn:         conn,
		Send:         make(chan []byte, ClientSendBufferSize),
		pingTick:     time.NewTicker(hub.Config.PingInterval),
		IP:           ip,
		LastActivity: time.Now().Unix(),
	}
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

	c.Conn.SetReadLimit(512)
	c.Conn.SetPongHandler(func(appData string) error {
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
		c.Conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Error("Write error", "error", err)
				return
			}
		case <-c.pingTick.C:
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

	switch msg.Type {
	case "trainer_join":
		c.handleTrainerJoin(msg)
	case "stagiaire_join":
		c.handleStagiaireJoin(msg)
	case "start_vote":
		c.handleStartVote(msg)
	case "vote":
		c.handleVote(msg)
	case "close_vote":
		c.handleCloseVote(msg)
	case "reset_vote":
		c.handleResetVote(msg)
	case "update_name":
		c.handleUpdateName(msg)
	default:
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
    if !vote.IsValidSessionCode(msg.SessionCode) {
        backoffMs := c.Hub.Security.RecordFailedJoin(c.IP)
        c.SendJSON(map[string]any{"type":"error", "message":"Invalid session code", "backoffMs": backoffMs})
        return
    }

    c.Type = "trainer"
    c.SessionID = msg.SessionCode
    c.Hub.Security.ClearFailedJoin(c.IP)
    
    select {
    case c.Hub.Register <- c:
        c.SendJSON(map[string]any{
            "type": "session_created",
            "sessionCode": msg.SessionCode,
        })
    case <-c.Hub.Context().Done():
        c.SendError("Server is shutting down")
    }
}

func (c *Client) handleStagiaireJoin(msg models.Message) {
    if !vote.IsValidSessionCode(msg.SessionCode) {
        c.SendErrorWithBackoff("Invalid session code")
        return
    }
    if !vote.IsValidStagiaireID(msg.StagiaireID) {
        c.SendErrorWithBackoff("Invalid stagiaire ID")
        return
    }
    if msg.Name != "" && !vote.IsValidName(msg.Name) {
        c.SendErrorWithBackoff("Invalid name")
        return
    }
    
    c.Type = "stagiaire"
    c.ID = msg.StagiaireID
    c.Name = msg.Name
    c.SessionID = msg.SessionCode
    
    // Check if session exists via Hub (which checks Manager/Connections)
    if !c.Hub.SessionExists(c.SessionID) {
        c.SendErrorWithBackoff("Session not found")
        return
    }

    c.Hub.Security.ClearFailedJoin(c.IP)
    
    select {
    case c.Hub.Register <- c:
        c.SendJSON(map[string]any{
            "type": "session_joined",
            "sessionCode": msg.SessionCode,
        })
    case <-c.Hub.Context().Done():
        c.SendError("Server is shutting down")
    }
}

func (c *Client) SendErrorWithBackoff(msg string) {
    backoffMs := c.Hub.Security.RecordFailedJoin(c.IP)
    c.SendJSON(map[string]any{
        "type": "error",
        "message": msg,
        "backoffMs": backoffMs,
    })
}

func (c *Client) handleStartVote(msg models.Message) {
    err := c.Hub.VoteManager.StartVote(c.SessionID, c.ID, msg.Colors, msg.MultipleChoice)
    
    if err != nil {
        c.SendError(err.Error())
        return
    }
    
    c.Hub.BroadcastSession(c.SessionID, map[string]any{
        "type": "vote_started",
        "colors": msg.Colors,
        "multipleChoice": msg.MultipleChoice,
    }, "")
}

func (c *Client) handleVote(msg models.Message) {
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
        "type": "vote_received",
        "stagiaireId": c.ID,
        "stagiaireName": stagiaireName,
        "colors": msg.Colors,
    })
}

func (c *Client) handleCloseVote(_ models.Message) {
    err := c.Hub.VoteManager.CloseVote(c.SessionID, c.ID)
    if err != nil {
        return 
    }
    c.Hub.BroadcastSession(c.SessionID, map[string]any{"type": "vote_closed"}, "")
}

func (c *Client) handleResetVote(msg models.Message) {
    err := c.Hub.VoteManager.ResetVote(c.SessionID, c.ID, msg.Colors, msg.MultipleChoice)
    if err != nil {
        c.SendError(err.Error())
        return
    }
    c.Hub.BroadcastSession(c.SessionID, map[string]any{"type": "vote_reset"}, "")
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
