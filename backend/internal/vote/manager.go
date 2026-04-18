package vote

import (
	"errors"
	"strings"
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

// SessionExists checks if a session with the given ID exists
func (m *Manager) SessionExists(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.sessions[sessionID]
	return ok
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
	session.VoteStartTime = time.Now().Unix()
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

	if !session.MultipleChoice && len(colors) > 1 {
		return "", errors.New("only one color allowed in single-choice mode")
	}

	if len(colors) == 0 {
		return "", errors.New("at least one color is required")
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
	session.VoteStartTime = 0
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

	if _, exists := session.Stagiaires[stagiaireID]; !exists {
		return errors.New("stagiaire not found in session")
	}

	// Check for name collision
	normalizedNew := normalizeName(name)
	for id, n := range session.Stagiaires {
		if id != stagiaireID && normalizeName(n) == normalizedNew {
			return errors.New("Ce nom est déjà utilisé")
		}
	}

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
		inactive := now-session.LastActivity > timeoutSec
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

func (m *Manager) GetAllSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

func (m *Manager) GetSessionIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// StagiaireExists checks if a stagiaire ID exists in any session
func (m *Manager) StagiaireExists(stagiaireID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		session.mu.RLock()
		_, exists := session.Stagiaires[stagiaireID]
		session.mu.RUnlock()
		if exists {
			return true
		}
	}
	return false
}

// GetStagiaireIDByName checks if a stagiaire name already exists in the session and returns their ID
func (m *Manager) GetStagiaireIDByName(sessionID, name string) (string, bool) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return "", false
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	normalizedNew := normalizeName(name)
	for id, n := range session.Stagiaires {
		if normalizeName(n) == normalizedNew {
			return id, true
		}
	}
	return "", false
}

// IsNameInUse checks if a normalized name exists in the session, excluding a specific ID
func (m *Manager) IsNameInUse(sessionID, name string, excludeID string) bool {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return false
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	normalizedNew := normalizeName(name)
	for id, n := range session.Stagiaires {
		if id != excludeID && normalizeName(n) == normalizedNew {
			return true
		}
	}
	return false
}

func normalizeName(name string) string {
	name = strings.ToLower(name)

	// Remove accents
	var b strings.Builder
	for _, r := range name {
		switch r {
		case 'à', 'â', 'ä':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë':
			b.WriteRune('e')
		case 'î', 'ï':
			b.WriteRune('i')
		case 'ô', 'ö':
			b.WriteRune('o')
		case 'ù', 'û', 'ü':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		case ' ', '-':
			// Skip spaces and hyphens
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
