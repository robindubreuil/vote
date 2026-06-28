package vote

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"vote-backend/internal/models"
)

const (
	PointsPerCorrect = 2000
	PointsPerWrong   = -500
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrInvalidInput    = errors.New("invalid input")
)

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	stats    *ProductStats
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		stats:    NewProductStats(),
	}
}

// Stats returns the aggregate usage counters. The pointer is valid for the
// Manager's lifetime and safe to read concurrently.
func (m *Manager) Stats() *ProductStats { return m.stats }

func (m *Manager) CreateSession(sessionID, trainerID string) (*Session, error) {
	if !IsValidSessionCode(sessionID) {
		return nil, ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session := NewSession(sessionID, trainerID)
	m.sessions[sessionID] = session
	m.stats.SessionsCreated.Inc()
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
	m.stats.TraineesJoined.Inc()
	return nil
}

func (m *Manager) StartVote(sessionID, trainerID string, colors []string, multipleChoice bool, labels map[string]string, gameEnabled bool, competitive bool, allowBlank bool) error {
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
	session.ActiveLabels = labels
	session.MultipleChoice = multipleChoice
	session.GameEnabled = gameEnabled
	session.Competitive = competitive
	session.AllowBlank = allowBlank
	session.CorrectColors = nil
	session.Revealed = false
	session.LastVoteScores = make(map[string]int)
	session.Votes = make(map[string][]string)
	session.VoteStartTime = time.Now().Unix()
	session.LastActivity = time.Now().Unix()

	m.stats.VotesStarted.Inc()
	if gameEnabled {
		m.stats.GameEnabledVotes.Inc()
	}
	if multipleChoice {
		m.stats.MultipleChoiceVotes.Inc()
	}

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

	hasBlank := false
	for _, c := range colors {
		if c == "blank" {
			hasBlank = true
			break
		}
	}
	if hasBlank {
		if !session.AllowBlank {
			return "", errors.New("blank votes are not allowed")
		}
		if len(colors) > 1 {
			return "", errors.New("blank vote cannot be combined with other colors")
		}
	} else if len(colors) == 0 {
		return "", errors.New("at least one color is required")
	}

	// Validate colors against active colors (O(N^2) but N is small)
	activeSet := make(map[string]bool)
	for _, c := range session.ActiveColors {
		activeSet[c] = true
	}

	for _, c := range colors {
		if c == "blank" {
			continue
		}
		if !activeSet[c] {
			return "", errors.New("invalid color: " + c)
		}
	}

	session.Votes[stagiaireID] = colors
	session.LastActivity = time.Now().Unix()
	m.stats.VotesCast.Inc()

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

func (m *Manager) ResetVote(sessionID, trainerID string, colors []string, multipleChoice bool, labels map[string]string, gameEnabled bool, competitive bool, allowBlank bool) error {
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
	session.ActiveLabels = labels
	session.MultipleChoice = multipleChoice
	session.GameEnabled = gameEnabled
	session.Competitive = competitive
	session.AllowBlank = allowBlank
	session.CorrectColors = nil
	session.Revealed = false
	session.LastVoteScores = make(map[string]int)
	session.Votes = make(map[string][]string)
	session.VoteStartTime = 0
	session.LastActivity = time.Now().Unix()
	return nil
}

type ScoreEntry struct {
	StagiaireID string   `json:"id"`
	Name        string   `json:"name"`
	Vote        []string `json:"vote,omitempty"`
	VoteScore   int      `json:"voteScore"`
	TotalScore  int      `json:"totalScore"`
	Rank        int      `json:"rank"`
}

func (m *Manager) RevealAnswers(sessionID, trainerID string, correctColors []string) ([]ScoreEntry, error) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.TrainerID != trainerID {
		return nil, ErrUnauthorized
	}

	if session.VoteState != models.VoteStateClosed {
		return nil, errors.New("vote must be closed before revealing answers")
	}

	correctSet := make(map[string]bool, len(correctColors))
	for _, c := range correctColors {
		correctSet[c] = true
	}

	session.CorrectColors = correctColors

	entries := make([]ScoreEntry, 0, len(session.Stagiaires))
	for id, name := range session.Stagiaires {
		entry := ScoreEntry{StagiaireID: id, Name: name}
		if vote, hasVote := session.Votes[id]; hasVote {
			entry.Vote = vote
			for _, color := range vote {
				if color == "blank" {
					continue
				}
				if correctSet[color] {
					entry.VoteScore += PointsPerCorrect
				} else {
					entry.VoteScore += PointsPerWrong
				}
			}
		}
		if session.Revealed {
			session.Scores[id] -= session.LastVoteScores[id]
		}
		session.Scores[id] += entry.VoteScore
		session.LastVoteScores[id] = entry.VoteScore
		entry.TotalScore = session.Scores[id] + session.GameScores[id]
		entries = append(entries, entry)
	}

	session.Revealed = true

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TotalScore != entries[j].TotalScore {
			return entries[i].TotalScore > entries[j].TotalScore
		}
		return entries[i].Name < entries[j].Name
	})

	for i := range entries {
		entries[i].Rank = i + 1
	}

	session.LastActivity = time.Now().Unix()
	return entries, nil
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
	normalizedNew := NormalizeName(name)
	for id, n := range session.Stagiaires {
		if id != stagiaireID && NormalizeName(n) == normalizedNew {
			return errors.New("Ce nom est déjà utilisé") //nolint:staticcheck // user-facing French message
		}
	}

	session.Stagiaires[stagiaireID] = name
	session.LastActivity = time.Now().Unix()
	return nil
}

func (m *Manager) CleanupExpiredSessions(timeout time.Duration, protected map[string]bool) {
	now := time.Now().Unix()
	timeoutSec := int64(timeout.Seconds())

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, session := range m.sessions {
		if protected[id] {
			continue
		}
		session.mu.RLock()
		inactive := now-session.LastActivity > timeoutSec
		if inactive {
			m.stats.observeEndedSession(session.CreatedAt, len(session.Votes), len(session.Stagiaires))
		}
		session.mu.RUnlock()
		if inactive {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, ok := m.sessions[sessionID]; ok {
		session.mu.RLock()
		m.stats.observeEndedSession(session.CreatedAt, len(session.Votes), len(session.Stagiaires))
		session.mu.RUnlock()
	}
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

	normalizedNew := NormalizeName(name)
	for id, n := range session.Stagiaires {
		if NormalizeName(n) == normalizedNew {
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

	normalizedNew := NormalizeName(name)
	for id, n := range session.Stagiaires {
		if id != excludeID && NormalizeName(n) == normalizedNew {
			return true
		}
	}
	return false
}

func (m *Manager) UpdateGameScore(sessionID, stagiaireID string, score int) error {
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

	if score > session.GameScores[stagiaireID] {
		session.GameScores[stagiaireID] = score
	}
	session.LastActivity = time.Now().Unix()
	return nil
}

func NormalizeName(name string) string {
	name = strings.ToLower(name)

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
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
