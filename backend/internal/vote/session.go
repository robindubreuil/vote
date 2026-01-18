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
	VoteStartTime  int64               // Unix timestamp when vote was started
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

// GetVote returns the vote for a specific stagiaire
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
