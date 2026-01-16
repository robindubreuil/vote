package vote

import (
	"errors"
	"sync"
	"time"
	"vote-backend/internal/models"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrInvalidInput    = errors.New("invalid input")
)

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) CreateSession(sessionID, trainerID string) (*Session, error) {
	if !IsValidSessionCode(sessionID) {
		return nil, ErrInvalidInput
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	session := NewSession(sessionID, trainerID)
	m.sessions[sessionID] = session
	return session, nil
}

func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	s, ok := m.sessions[sessionID]
	return s, ok
}

func (m *Manager) UpdateTrainer(sessionID, trainerID string) error {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	
	session.TrainerID = trainerID
	session.LastActivity = time.Now().Unix()
	return nil
}

func (m *Manager) JoinStagiaire(sessionID, stagiaireID, name string) error {
	if !IsValidSessionCode(sessionID) || !IsValidStagiaireID(stagiaireID) {
		return ErrInvalidInput
	}
	if name != "" && !IsValidName(name) {
		return ErrInvalidInput
	}

	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	session.Stagiaires[stagiaireID] = name
	session.LastActivity = time.Now().Unix()
	return nil
}

func (m *Manager) StartVote(sessionID, trainerID string, colors []string, multipleChoice bool) error {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.TrainerID != trainerID {
		return ErrUnauthorized
	}

	session.VoteState = models.VoteStateActive
	session.ActiveColors = colors
	session.MultipleChoice = multipleChoice
	session.Votes = make(map[string][]string)
	session.LastActivity = time.Now().Unix()

	return nil
}

func (m *Manager) SubmitVote(sessionID, stagiaireID string, colors []string) (string, error) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	
	if !ok {
		return "", ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

    if session.VoteState != models.VoteStateActive {
        return "", errors.New("vote is not active")
    }

	// Validate colors against active colors (O(N^2) but N is small)
	activeSet := make(map[string]bool)
	for _, c := range session.ActiveColors {
		activeSet[c] = true
	}

	for _, c := range colors {
		if !activeSet[c] {
			return "", errors.New("invalid color: " + c)
		}
	}

	session.Votes[stagiaireID] = colors
	session.LastActivity = time.Now().Unix()

	stagiaireName := session.Stagiaires[stagiaireID]
	return stagiaireName, nil
}

func (m *Manager) CloseVote(sessionID, trainerID string) error {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.TrainerID != trainerID {
		return ErrUnauthorized
	}

	session.VoteState = models.VoteStateClosed
	session.LastActivity = time.Now().Unix()
	return nil
}

func (m *Manager) ResetVote(sessionID, trainerID string, colors []string, multipleChoice bool) error {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.TrainerID != trainerID {
		return ErrUnauthorized
	}

	session.VoteState = models.VoteStateIdle
	if len(colors) > 0 {
		session.ActiveColors = colors
	} else {
		session.ActiveColors = []string{}
	}
	session.MultipleChoice = multipleChoice
	session.Votes = make(map[string][]string)
	session.LastActivity = time.Now().Unix()
	return nil
}

func (m *Manager) UpdateStagiaireName(sessionID, stagiaireID, name string) error {
    if !IsValidName(name) {
        return ErrInvalidInput
    }
	
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	
    if !ok {
        return ErrSessionNotFound
    }

	session.mu.Lock()
	defer session.mu.Unlock()

    session.Stagiaires[stagiaireID] = name
    session.LastActivity = time.Now().Unix()
    return nil
}

func (m *Manager) CleanupExpiredSessions(timeout time.Duration) {
    now := time.Now().Unix()
    timeoutSec := int64(timeout.Seconds())
    
    var expiredSessions []string
	m.mu.RLock()
    for id, session := range m.sessions {
		session.mu.RLock()
		inactive := now - session.LastActivity > timeoutSec
		session.mu.RUnlock()
		
        if inactive {
            expiredSessions = append(expiredSessions, id)
        }
    }
	m.mu.RUnlock()

	if len(expiredSessions) > 0 {
		m.mu.Lock()
		for _, id := range expiredSessions {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
	}
}

func (m *Manager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
    delete(m.sessions, sessionID)
}
