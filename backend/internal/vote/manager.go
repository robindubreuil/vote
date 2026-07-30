package vote

import (
	"crypto/subtle"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"vote-backend/internal/models"
	"vote-backend/internal/security"
)

const (
	PointsPerCorrect = 2000
	PointsPerWrong   = -500
)

var (
	ErrSessionNotFound = errors.New("session not found")
	// ErrUnauthorized is the trainer-role gate sentinel. The string is
	// internal-facing; callers that surface it to a client must map it
	// through UserFacingError (B3) so the wire stays French.
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidInput = errors.New("invalid input")
	// ErrNameInUse is returned by JoinStagiaire when a normalised name
	// collides with another stagiaire. The advisory check in
	// handleStagiaireJoin runs in the client goroutine while the actual
	// map write happens later in Hub.Run; without this authoritative
	// re-check under the session lock, two clients racing for the same
	// name both pass the advisory check and both register (CC2).
	ErrNameInUse = errors.New("name already in use")
	// ErrVoteAlreadyActive is returned by StartVote when a vote is
	// already in progress. A second start_vote would silently discard
	// every collected vote (BM2).
	ErrVoteAlreadyActive = errors.New("un vote est déjà en cours")
	// ErrReclaimUnauthorized is returned by JoinStagiaire when a
	// stagiaireId is presented that matches an existing entry but the
	// reclaim token is wrong or missing (S6/S12). The caller must drop
	// the stale ID and rejoin with a fresh identity.
	ErrReclaimUnauthorized = errors.New("reclaim token required")

	// B3: French sentinels for leaf errors that previously reached
	// clients as raw English (or leaked internal entity names like
	// "stagiaire"). The hub's SendError path forwards err.Error()
	// verbatim, so the sentinel string is what the UI toast shows.
	// These are also matched by UserFacingError so handler-level
	// mapping can stay terse: any error from the manager surface that
	// isn't a known sentinel falls back to a generic French message.
	ErrVoteNotActive     = errors.New("Aucun vote en cours")
	ErrSingleChoiceOnly  = errors.New("Un seul choix est autorisé")
	ErrDuplicateColors   = errors.New("Couleurs en double interdites")
	ErrBlankNotAllowed   = errors.New("Le vote blanc n'est pas autorisé")
	ErrBlankWithColors   = errors.New("Le vote blanc ne peut pas être combiné à d'autres couleurs")
	ErrAtLeastOneColor   = errors.New("Au moins une couleur est requise")
	ErrInvalidColor      = errors.New("Couleur invalide")
	ErrVoteNotClosed     = errors.New("Le vote doit être clôturé avant la révélation")
	ErrStagiaireNotFound = errors.New("Stagiaire introuvable")
	ErrNotAuthorized     = errors.New("Action non autorisée")
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
	session.TrainerToken = security.GenerateToken()
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

// ValidateTrainerToken reports whether the presented token matches the
// session's minted trainer token. Constant-time compare prevents timing
// side-channels. A session with an empty TrainerToken (only possible for
// legacy in-memory sessions pre-dating the field) matches nothing, which
// forces token-less legacy sessions to be recreated rather than silently
// treated as public.
func (m *Manager) ValidateTrainerToken(sessionID, token string) bool {
	if token == "" {
		return false
	}
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.TrainerToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(session.TrainerToken), []byte(token)) == 1
}

// JoinStagiaireResult carries the outcome of a successful join. The
// ReclaimToken must be returned to the client so it can prove ownership
// of the identity on future reconnects (S6/S12). Reclaimed is true when
// the join attached to an existing identity (preserving Scores and
// GameScores); false when a fresh identity was minted.
type JoinStagiaireResult struct {
	ReclaimToken string
	Reclaimed    bool
}

func (m *Manager) JoinStagiaire(sessionID, stagiaireID, name, reclaimToken string) (JoinStagiaireResult, error) {
	if !IsValidSessionCode(sessionID) || !IsValidStagiaireID(stagiaireID) {
		return JoinStagiaireResult{}, ErrInvalidInput
	}
	if name != "" && !IsValidName(name) {
		return JoinStagiaireResult{}, ErrInvalidInput
	}

	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return JoinStagiaireResult{}, ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	// S6/S12: if the ID is already in the session, the presented
	// reclaim token must match the stored one. This is the sole
	// authority for identity reclaim — name-based matching was removed
	// because names are public, guessable, and ≤16 chars (a "Marie"
	// could be anyone). The ID alone is also insufficient: it is
	// visible to anyone who can read the stagiaire's sessionStorage on
	// a shared device. The token is the cryptographic proof of
	// ownership; constant-time compare prevents timing side-channels.
	if _, exists := session.Stagiaires[stagiaireID]; exists {
		stored := session.ReclaimTokens[stagiaireID]
		if stored == "" || subtle.ConstantTimeCompare([]byte(stored), []byte(reclaimToken)) != 1 {
			return JoinStagiaireResult{}, ErrReclaimUnauthorized
		}
		// S15: the reclaim-rename path must re-run the authoritative
		// name-collision check (CC2) under the session lock, excluding
		// the reclaimer's own ID. Previously only the fresh-join branch
		// checked, so the advisory IsNameInUse lookup running in the
		// client goroutine was the sole guard — exactly the TOCTOU
		// pattern CC2 eliminated. Two stagiaires could end up sharing a
		// normalised name, breaking the uniqueness invariant that gates
		// ranking/leaderboard tie-breakers (Sort by Name ASC +
		// AssignCompetitionRanks) and showing two indistinguishable rows
		// in the trainer view.
		if name != "" && nameCollidesLocked(session, name, stagiaireID) {
			return JoinStagiaireResult{}, ErrNameInUse
		}
		// Reconnect path: the identity is reclaimed, not minted. Apply
		// the (possibly updated) name, then bail out without running
		// the new-join name-collision check below.
		if name != "" {
			session.Stagiaires[stagiaireID] = name
		}
		session.LastActivity = time.Now().Unix()
		return JoinStagiaireResult{ReclaimToken: stored, Reclaimed: true}, nil
	}

	// Fresh join path. Mint a reclaim token alongside the new entry so
	// the invariant (id ∈ Stagiaires ⟺ id ∈ ReclaimTokens) holds.
	// S15: the collision check is the same helper the reclaim branch
	// uses, so both branches enforce the uniqueness invariant under the
	// session lock with no advisory-only TOCTOU gap.
	if name != "" && nameCollidesLocked(session, name, stagiaireID) {
		return JoinStagiaireResult{}, ErrNameInUse
	}

	token := security.GenerateToken()
	session.Stagiaires[stagiaireID] = name
	session.ReclaimTokens[stagiaireID] = token
	session.LastActivity = time.Now().Unix()
	m.stats.TraineesJoined.Inc()
	return JoinStagiaireResult{ReclaimToken: token, Reclaimed: false}, nil
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

	// BM2: a second start_vote while a vote is already Active would
	// silently wipe the in-progress Votes map (and LastVoteScores),
	// losing every collected vote. Force the trainer to close or reset
	// first. Closed → StartVote is the legitimate "next round" path and
	// remains allowed.
	if session.VoteState == models.VoteStateActive {
		return ErrVoteAlreadyActive
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
		return "", ErrVoteNotActive
	}

	if !session.MultipleChoice && len(colors) > 1 {
		return "", ErrSingleChoiceOnly
	}

	// BL6: duplicate-color check lives inside SubmitVote (under the
	// session lock) so the business rule can't be bypassed by a caller
	// that skips the hub handler. The handler-side check is a useful
	// fast-fail but is not authoritative.
	if HasDuplicates(colors) {
		return "", ErrDuplicateColors
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
			return "", ErrBlankNotAllowed
		}
		if len(colors) > 1 {
			return "", ErrBlankWithColors
		}
	} else if len(colors) == 0 {
		return "", ErrAtLeastOneColor
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
			return "", ErrInvalidColor
		}
	}

	session.Votes[stagiaireID] = colors
	session.TotalVotes++
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
		return nil, ErrVoteNotClosed
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
		// BL3: scoring (cumulative accumulation + idempotent reversal)
		// is scoped to Competitive mode by design. The per-round
		// VoteScore above is still computed so the response carries
		// correctness info, but a non-competitive session never mutates
		// Scores / LastVoteScores / Revealed — those exist solely to
		// power the competitive leaderboard across rounds.
		if session.Competitive {
			if session.Revealed {
				session.Scores[id] -= session.LastVoteScores[id]
			}
			session.Scores[id] += entry.VoteScore
			session.LastVoteScores[id] = entry.VoteScore
		}
		entry.TotalScore = session.Scores[id] + session.GameScores[id]
		entries = append(entries, entry)
	}

	if session.Competitive {
		session.Revealed = true
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TotalScore != entries[j].TotalScore {
			return entries[i].TotalScore > entries[j].TotalScore
		}
		return entries[i].Name < entries[j].Name
	})

	// BL2: assign competition ranks (tied TotalScores share a rank, the
	// next lower score skips) so the value sent at reveal matches what
	// computeRank will produce on reconnect. The previous ordinal
	// assignment (i+1) broke ties alphabetically, so a student tied for
	// first could see "2e" at reveal then "1er" after a reconnect.
	AssignCompetitionRanks(entries)

	session.LastActivity = time.Now().Unix()
	return entries, nil
}

// AssignCompetitionRanks sets Rank on a slice pre-sorted by (TotalScore
// DESC, tiebreaker ASC) using competition ranking: entries that tie on
// TotalScore share a rank, and the next lower score's rank reflects the
// number of entries strictly ahead rather than i+1. This matches the
// on-the-fly computation in hub.computeRank so a client sees the same
// rank at reveal and on reconnect (BL2). R4: exported so the hub's
// buildScoreboard (which feeds trainer reconnect) uses the same ranking
// as RevealAnswers — previously it assigned ordinal ranks, so a tied
// trainer view flipped between "1er, 2e, 3e" (reconnect) and "1er, 1er,
// 3e" (reveal).
//
// Precondition: entries is sorted by (TotalScore DESC, Name ASC).
func AssignCompetitionRanks(entries []ScoreEntry) {
	for i := range entries {
		if i > 0 && entries[i].TotalScore == entries[i-1].TotalScore {
			entries[i].Rank = entries[i-1].Rank
		} else {
			entries[i].Rank = i + 1
		}
	}
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
		return ErrStagiaireNotFound
	}

	// Check for name collision
	// S15: shared with JoinStagiaire so every name mutation enforces the
	// uniqueness invariant under the session lock.
	if nameCollidesLocked(session, name, stagiaireID) {
		return ErrNameInUse
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
			// BM4: observe lifetime votes (TotalVotes), not len(Votes)
			// which only reflects the current/last round.
			m.stats.observeEndedSession(session.CreatedAt, session.TotalVotes, len(session.Stagiaires))
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
		// BM4: observe lifetime votes (TotalVotes), not len(Votes)
		// which only reflects the current/last round.
		m.stats.observeEndedSession(session.CreatedAt, session.TotalVotes, len(session.Stagiaires))
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

	// S15: delegate to the shared locked helper so the advisory check
	// the client goroutine performs cannot diverge from the
	// authoritative check JoinStagiaire / UpdateStagiaireName run under
	// the session lock.
	return nameCollidesLocked(session, name, excludeID)
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
		return ErrStagiaireNotFound
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

// nameCollidesLocked reports whether a normalised form of name is
// already held by any stagiaire other than excludeID. Caller must hold
// session.mu (R or W). S15 factored this out of the fresh-join branch so
// the reclaim-rename path and UpdateStagiaireName enforce the same
// uniqueness invariant under the lock, with no advisory-only TOCTOU gap
// between the client-goroutine check and the authoritative map write.
func nameCollidesLocked(session *Session, name, excludeID string) bool {
	normalised := NormalizeName(name)
	for id, n := range session.Stagiaires {
		if id != excludeID && NormalizeName(n) == normalised {
			return true
		}
	}
	return false
}
