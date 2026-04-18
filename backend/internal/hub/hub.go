package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/models"
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

func (h *Hub) IsStagiaireConnected(sessionID, stagiaireID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	conns, ok := h.Connections[sessionID]
	if !ok {
		return false
	}
	_, connected := conns.Stagiaires[stagiaireID]
	return connected
}

func (h *Hub) GenerateSessionCode() string {
	for i := 0; i < 100; i++ {
		code := fmt.Sprintf("%04d", rand.IntN(10000))
		if !h.SessionExists(code) && !h.VoteManager.SessionExists(code) {
			return code
		}
	}

	used := make(map[string]bool)

	h.mu.RLock()
	for id := range h.Connections {
		used[id] = true
	}
	h.mu.RUnlock()

	vmIDs := h.VoteManager.GetSessionIDs()
	for _, id := range vmIDs {
		used[id] = true
	}

	for i := 0; i < 10000; i++ {
		code := fmt.Sprintf("%04d", i)
		if !used[code] {
			return code
		}
	}

	return ""
}

func (h *Hub) GenerateUniqueClientID() string {
	for i := 0; i < 10; i++ {
		id := security.GenerateID()
		if !h.ClientIDExists(id) {
			return id
		}
	}
	for i := 0; i < 1000; i++ {
		id := security.GenerateID()
		if !h.ClientIDExists(id) {
			return id
		}
	}
	return ""
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

		h.notifyTrainerStagiaireListLocked(conns, client.SessionID, "connected_count")

		if session, ok := h.VoteManager.GetSession(client.SessionID); ok {
			state, colors, multipleChoice, voteStartTime := session.GetState()

			if state == models.VoteStateActive || state == models.VoteStateClosed {
				client.SendJSON(map[string]any{
					"type":           "vote_started",
					"colors":         colors,
					"multipleChoice": multipleChoice,
					"voteStartTime":  voteStartTime,
				})

				votes := session.GetVotes()
				stagiaires := session.GetStagiaires()
				for sID, vColors := range votes {
					sName := stagiaires[sID]
					client.SendJSON(map[string]any{
						"type":          "vote_received",
						"stagiaireId":   sID,
						"stagiaireName": sName,
						"colors":        vColors,
					})
				}

				if state == models.VoteStateClosed {
					client.SendJSON(map[string]any{"type": "vote_closed"})
				}
			} else {
				client.SendJSON(map[string]any{
					"type":           "config_updated",
					"selectedColors": colors,
					"multipleChoice": multipleChoice,
				})
			}
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

		h.notifyTrainerStagiaireListLocked(conns, client.SessionID, "connected_count")

		session, ok := h.VoteManager.GetSession(client.SessionID)
		if ok {
			state, colors, multipleChoice, _ := session.GetState()
			if state == models.VoteStateActive {
				msg := map[string]any{
					"type":           "vote_started",
					"colors":         colors,
					"multipleChoice": multipleChoice,
				}
				if existingVote, hasVoted := session.GetVote(client.ID); hasVoted {
					msg["existingVote"] = existingVote
				}
				client.SendJSON(msg)
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
			h.notifyTrainerStagiaireListLocked(conns, client.SessionID, "connected_count")
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

	for _, c := range targets {
		select {
		case c.Send <- data:
		default:
			slog.Warn("Broadcast dropped: channel full", "client_id", c.ID)
		}
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

func (h *Hub) NotifyTrainerStagiaireList(sessionID string, msgType string) {
	h.mu.RLock()
	conns, exists := h.Connections[sessionID]
	if !exists {
		h.mu.RUnlock()
		return
	}
	h.notifyTrainerStagiaireListLocked(conns, sessionID, msgType)
	h.mu.RUnlock()
}

func (h *Hub) notifyTrainerStagiaireListLocked(conns *SessionConnections, sessionID string, msgType string) {
	if conns.Trainer == nil {
		return
	}

	session, ok := h.VoteManager.GetSession(sessionID)
	if !ok {
		return
	}

	stagiaires := session.GetStagiaires()
	votes := session.GetVotes()

	type StagiaireInfo struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Connected bool     `json:"connected"`
		Vote      []string `json:"vote,omitempty"`
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
		list = append(list, info)
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
			for id, conns := range h.Connections {
				if _, ok := h.VoteManager.GetSession(id); !ok {
					if conns.Trainer != nil || len(conns.Stagiaires) > 0 {
						h.VoteManager.CreateSession(id, "")
					} else {
						delete(h.Connections, id)
					}
				}
			}
			h.mu.Unlock()
		case <-h.ctx.Done():
			return
		}
	}
}

type Metrics struct {
	ActiveSessions      int            `json:"active_sessions"`
	ConnectedTrainers   int            `json:"connected_trainers"`
	ConnectedStagiaires int            `json:"connected_stagiaires"`
	VoteStates          map[string]int `json:"vote_states"`
}

func (h *Hub) GetMetrics() Metrics {
	h.mu.RLock()
	defer h.mu.RUnlock()

	metrics := Metrics{
		ActiveSessions:      len(h.Connections),
		ConnectedTrainers:   0,
		ConnectedStagiaires: 0,
		VoteStates: map[string]int{
			"idle":   0,
			"active": 0,
			"closed": 0,
		},
	}

	for _, conn := range h.Connections {
		if conn.Trainer != nil {
			metrics.ConnectedTrainers++
		}
		metrics.ConnectedStagiaires += len(conn.Stagiaires)
	}

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
