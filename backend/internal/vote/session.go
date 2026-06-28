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
	Stagiaires     map[string]string
	VoteState      string
	ActiveColors   []string
	ActiveLabels   map[string]string
	MultipleChoice bool
	GameEnabled    bool
	Votes          map[string][]string
	VoteStartTime  int64
	CreatedAt      int64
	LastActivity   int64
}

func NewSession(id, trainerID string) *Session {
	now := time.Now().Unix()
	return &Session{
		ID:           id,
		TrainerID:    trainerID,
		Stagiaires:   make(map[string]string),
		VoteState:    models.VoteStateIdle,
		Votes:        make(map[string][]string),
		CreatedAt:    now,
		LastActivity: now,
	}
}

func (s *Session) GetState() (string, []string, bool, int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	colors := make([]string, len(s.ActiveColors))
	copy(colors, s.ActiveColors)
	return s.VoteState, colors, s.MultipleChoice, s.VoteStartTime
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

func (s *Session) GetVotes() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	votes := make(map[string][]string, len(s.Votes))
	for k, v := range s.Votes {
		vCopy := make([]string, len(v))
		copy(vCopy, v)
		votes[k] = vCopy
	}
	return votes
}

func (s *Session) GetVote(stagiaireID string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vote, exists := s.Votes[stagiaireID]
	if !exists {
		return nil, false
	}

	vCopy := make([]string, len(vote))
	copy(vCopy, vote)
	return vCopy, true
}

func (s *Session) GetActiveLabels() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.ActiveLabels) == 0 {
		return nil
	}
	labels := make(map[string]string, len(s.ActiveLabels))
	for k, v := range s.ActiveLabels {
		labels[k] = v
	}
	return labels
}

func (s *Session) GetGameEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GameEnabled
}
