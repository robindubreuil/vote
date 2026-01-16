package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/security"
	"vote-backend/internal/vote"
)

type SessionConnections struct {
	Trainer    *Client
	Stagiaires map[string]*Client
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
}

func NewHub(cfg *config.Config) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		Connections: make(map[string]*SessionConnections),
		VoteManager: vote.NewManager(),
		Security:    security.NewSecurity(ctx),
		Register:    make(chan *Client),
		Unregister:  make(chan *Client),
		ctx:         ctx,
		cancel:      cancel,
		Config:      cfg,
	}
}

func (h *Hub) Context() context.Context {
	return h.ctx
}

func (h *Hub) Run() {
	go h.cleanupLoop()
	for {
		select {
		case client := <-h.Register:
			h.registerClient(client)
		case client := <-h.Unregister:
			h.unregisterClient(client)
		case <-h.ctx.Done():
			return
		}
	}
}

func (h *Hub) Shutdown() {
	h.Security.Shutdown()
	h.cancel()
}

func (h *Hub) SessionExists(sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.Connections[sessionID]
	return ok
}

func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conns, exists := h.Connections[client.SessionID]
	if !exists {
		if client.Type == "trainer" {
			conns = &SessionConnections{
				Stagiaires: make(map[string]*Client),
			}
			h.Connections[client.SessionID] = conns
			h.VoteManager.CreateSession(client.SessionID, client.ID)
		} else {
			client.SendError("Session not found")
			return
		}
	}

	if client.Type == "trainer" {
		if conns.Trainer != nil && conns.Trainer != client {
			conns.Trainer.SendError("New trainer connection detected, closing this one.")
			go func(oldClient *Client) {
				time.Sleep(100 * time.Millisecond)
				oldClient.Conn.Close()
			}(conns.Trainer)
		}

		conns.Trainer = client
		if _, ok := h.VoteManager.GetSession(client.SessionID); !ok {
			h.VoteManager.CreateSession(client.SessionID, client.ID)
		} else {
			h.VoteManager.UpdateTrainer(client.SessionID, client.ID)
		}
	} else {
		if conns.Trainer == nil {
			client.SendError("Session not available (no trainer)")
			return
		}

		if err := h.VoteManager.JoinStagiaire(client.SessionID, client.ID, client.Name); err != nil {
			client.SendError("Failed to join session: " + err.Error())
			return
		}

		if old, ok := conns.Stagiaires[client.ID]; ok {
			if old.Conn != nil {
				old.Conn.Close()
			}
		}
		conns.Stagiaires[client.ID] = client

		h.notifyTrainerStagiaireList(conns, client.SessionID, "connected_count")

		session, ok := h.VoteManager.GetSession(client.SessionID)
		if ok {
			state, colors, multipleChoice := session.GetState()
			if state == "active" {
				client.SendJSON(map[string]any{
					"type":           "vote_started",
					"colors":         colors,
					"multipleChoice": multipleChoice,
				})
			}
		}
	}
}

func (h *Hub) unregisterClient(client *Client) {
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
			h.notifyTrainerStagiaireList(conns, client.SessionID, "connected_count")
		}
	}
}

func (h *Hub) BroadcastSession(sessionID string, message any, excludeID string) {
	data, err := json.Marshal(message)
	if err != nil {
		slog.Error("Marshal error", "error", err)
		return
	}

	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	h.mu.RUnlock()

	if !exists {
		return
	}

	if conns.Trainer != nil && conns.Trainer.ID != excludeID {
		select {
		case conns.Trainer.Send <- data:
		default:
		}
	}

	for id, client := range conns.Stagiaires {
		if id != excludeID {
			select {
			case client.Send <- data:
			default:
			}
		}
	}
}

func (h *Hub) SendToTrainer(sessionID string, message any) {
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	h.mu.RUnlock()

	if !exists || conns.Trainer == nil {
		return
	}

	conns.Trainer.SendJSON(message)
}

func (h *Hub) NotifyTrainerStagiaireList(sessionID string, msgType string) {
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	h.mu.RUnlock()

	if exists {
		h.notifyTrainerStagiaireList(conns, sessionID, msgType)
	}
}

func (h *Hub) notifyTrainerStagiaireList(conns *SessionConnections, sessionID string, msgType string) {
	if conns.Trainer == nil {
		return
	}

	session, ok := h.VoteManager.GetSession(sessionID)
	if !ok {
		return
	}

	stagiaires := session.GetStagiaires()

	type StagiaireInfo struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
	}

	list := make([]StagiaireInfo, 0)
	for id, name := range stagiaires {
		_, connected := conns.Stagiaires[id]
		list = append(list, StagiaireInfo{
			ID:        id,
			Name:      name,
			Connected: connected,
		})
	}

	conns.Trainer.SendJSON(map[string]any{
		"type":       msgType,
		"count":      len(conns.Stagiaires),
		"stagiaires": list,
	})
}

func (h *Hub) cleanupLoop() {
	ticker := time.NewTicker(h.Config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.VoteManager.CleanupExpiredSessions(h.Config.SessionTimeout)
			h.mu.Lock()
			for id := range h.Connections {
				if _, ok := h.VoteManager.GetSession(id); !ok {
					delete(h.Connections, id)
				}
			}
			h.mu.Unlock()
		case <-h.ctx.Done():
			return
		}
	}
}