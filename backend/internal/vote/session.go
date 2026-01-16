package vote

import (
	"sync"
	"time"
	"vote-backend/internal/models"
)

type Session struct {
	mu             sync.RWMutex
	ID             string
	TrainerID      string
	Stagiaires     map[string]string // Map ID -> Name
	VoteState      string
	ActiveColors   []string
	MultipleChoice bool
	Votes          map[string][]string // Map ID -> Colors
	LastActivity   int64
}

func NewSession(id, trainerID string) *Session {
	return &Session{
		ID:           id,
		TrainerID:    trainerID,
		Stagiaires:   make(map[string]string),
		VoteState:    models.VoteStateIdle,
		Votes:        make(map[string][]string),
		LastActivity: time.Now().Unix(),
	}
}

func (s *Session) GetState() (string, []string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	colors := make([]string, len(s.ActiveColors))
	copy(colors, s.ActiveColors)
	return s.VoteState, colors, s.MultipleChoice
}

func (s *Session) GetStagiaires() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := make(map[string]string, len(s.Stagiaires))
	for k, v := range s.Stagiaires {
		st[k] = v
	}
	return st
}