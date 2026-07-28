package vote

import (
	"errors"
	"testing"
	"time"
	"vote-backend/internal/models"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m.sessions == nil {
		t.Fatal("sessions map should be initialized")
	}
}

func TestCreateSession(t *testing.T) {
	m := NewManager()

	// Valid creation
	session, err := m.CreateSession("ABC", "trainer1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if session == nil {
		t.Fatal("expected session to be returned")
	}
	if session.ID != "ABC" {
		t.Errorf("expected session ID 1234, got %s", session.ID)
	}
	if session.TrainerID != "trainer1" {
		t.Errorf("expected trainer ID trainer1, got %s", session.TrainerID)
	}

	// Invalid ID
	_, err = m.CreateSession("", "trainer1")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

// TestTrainerTokenMintingAndValidation covers S1: every session is minted a
// crypto-random trainer token that gates takeover of an active trainer.
func TestTrainerTokenMintingAndValidation(t *testing.T) {
	m := NewManager()

	sess, err := m.CreateSession("ABC", "trainer1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token := sess.GetTrainerToken()
	if token == "" {
		t.Fatal("session should be minted a non-empty trainer token")
	}

	// The token should have meaningful entropy (base64url of 32 bytes ≈ 43 chars).
	if len(token) < 32 {
		t.Errorf("trainer token too short (%d chars), expected ≥32", len(token))
	}

	// Correct token validates.
	if !m.ValidateTrainerToken("ABC", token) {
		t.Error("ValidateTrainerToken should accept the minted token")
	}

	// Wrong token does not validate.
	if m.ValidateTrainerToken("ABC", "wrong-token") {
		t.Error("ValidateTrainerToken should reject a wrong token")
	}

	// Empty token does not validate.
	if m.ValidateTrainerToken("ABC", "") {
		t.Error("ValidateTrainerToken should reject an empty token")
	}

	// Non-existent session does not validate.
	if m.ValidateTrainerToken("XYZ", token) {
		t.Error("ValidateTrainerToken should reject for a non-existent session")
	}

	// Each session gets a distinct token.
	sess2, _ := m.CreateSession("DEF", "trainer2")
	if sess2.GetTrainerToken() == token {
		t.Error("distinct sessions should have distinct trainer tokens")
	}
}

func TestJoinStagiaire(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	// Valid join - use exactly 12-char lowercase alphanumeric ID matching GenerateID format
	_, err := m.JoinStagiaire("ABC", "stag1ab12cde", "Jean", "")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.Stagiaires["stag1ab12cde"] != "Jean" {
		t.Errorf("expected name Jean, got %s", session.Stagiaires["stag1ab12cde"])
	}

	// Invalid session
	_, err = m.JoinStagiaire("KQR", "stag1ab12cde", "Jean", "")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	// Invalid Name
	_, err = m.JoinStagiaire("ABC", "stag1ab12cde", "<script>", "")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

// TestJoinStagiaireNameUniquenessUnderLock covers CC2: the authoritative
// name-collision check runs inside session.mu.Lock. Two distinct IDs
// submitting the same normalised name cannot both register.
func TestJoinStagiaireNameUniquenessUnderLock(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	res, err := m.JoinStagiaire("ABC", "s1abc1234567", "Jean", "")
	if err != nil {
		t.Fatalf("first join: %v", err)
	}

	// Same normalised name, different ID → collision.
	_, err = m.JoinStagiaire("ABC", "s2abc1234567", "jéan", "")
	if !errors.Is(err, ErrNameInUse) {
		t.Errorf("expected ErrNameInUse, got %v", err)
	}

	// Normalised-equal variants also collide.
	if _, err := m.JoinStagiaire("ABC", "s3abc1234567", "JEAN", ""); !errors.Is(err, ErrNameInUse) {
		t.Errorf("expected ErrNameInUse for case variant, got %v", err)
	}

	// Re-join with the SAME id and a valid token is a no-op reconnect,
	// not a collision. The minted token is the proof of ownership.
	if _, err := m.JoinStagiaire("ABC", "s1abc1234567", "Jean", res.ReclaimToken); err != nil {
		t.Errorf("same-id rejoin should succeed, got %v", err)
	}

	// Empty names never collide (anonymous stagiaires).
	if _, err := m.JoinStagiaire("ABC", "s4abc1234567", "", ""); err != nil {
		t.Errorf("empty name join should succeed, got %v", err)
	}
	if _, err := m.JoinStagiaire("ABC", "s5abc1234567", "", ""); err != nil {
		t.Errorf("second empty name join should succeed, got %v", err)
	}

	// A distinct name registers fine.
	if _, err := m.JoinStagiaire("ABC", "s6abc1234567", "Marie", ""); err != nil {
		t.Errorf("distinct name should succeed, got %v", err)
	}
}

func TestStartVote(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	colors := []string{"rouge", "bleu"}
	err := m.StartVote("ABC", "trainer1", colors, true, nil, false, false, false)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.VoteState != models.VoteStateActive {
		t.Errorf("expected active state, got %s", session.VoteState)
	}
	if len(session.ActiveColors) != 2 {
		t.Errorf("expected 2 active colors")
	}

	// Unauthorized trainer
	err = m.StartVote("ABC", "imposter", colors, true, nil, false, false, false)
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestStartVoteGameEnabled(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	if err := m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, true, false, false); err != nil {
		t.Fatalf("StartVote: %v", err)
	}
	session, _ := m.GetSession("ABC")
	if !session.GameEnabled {
		t.Errorf("expected GameEnabled=true after StartVote")
	}
	if !session.GetGameEnabled() {
		t.Errorf("expected GetGameEnabled()=true")
	}

	// ResetVote must propagate the flag too.
	if err := m.ResetVote("ABC", "trainer1", []string{"bleu"}, false, nil, true, false, false); err != nil {
		t.Fatalf("ResetVote: %v", err)
	}
	session, _ = m.GetSession("ABC")
	if !session.GameEnabled {
		t.Errorf("expected GameEnabled=true after ResetVote")
	}

	// Turning it off works.
	if err := m.StartVote("ABC", "trainer1", []string{"vert"}, false, nil, false, false, false); err != nil {
		t.Fatalf("StartVote: %v", err)
	}
	session, _ = m.GetSession("ABC")
	if session.GameEnabled {
		t.Errorf("expected GameEnabled=false after StartVote without flag")
	}
}

func TestSubmitVote(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, false, false)

	// Valid vote
	name, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if name != "Jean" {
		t.Errorf("expected name Jean, got %s", name)
	}

	session, _ := m.GetSession("ABC")
	if session.Votes["s1abc1234567"][0] != "rouge" {
		t.Errorf("expected vote rouge")
	}

	// Invalid color
	_, err = m.SubmitVote("ABC", "s1abc1234567", []string{"vert"})
	if err == nil {
		t.Error("expected error for invalid color")
	}

	// Vote when not active
	m.CloseVote("ABC", "trainer1")
	_, err = m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	if err == nil {
		t.Error("expected error when vote closed")
	}
}

func TestSubmitVoteSingleChoiceEnforcement(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge", "bleu"})
	if err == nil {
		t.Error("expected error when submitting multiple colors in single-choice mode")
	}
	if err.Error() != "only one color allowed in single-choice mode" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSubmitVoteMultipleChoiceAllowed(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, true, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge", "bleu"})
	if err != nil {
		t.Errorf("multiple colors should be allowed in multiple-choice mode, got: %v", err)
	}
}

func TestSubmitVoteEmptyColors(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{})
	if err == nil {
		t.Error("expected error when submitting empty colors")
	}
}

func TestUpdateStagiaireNameNonexistent(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	err := m.UpdateStagiaireName("ABC", "nonexistent1234", "Paul")
	if err == nil {
		t.Error("expected error when updating name for non-existent stagiaire")
	}
}

func TestResetVote(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false)

	err := m.ResetVote("ABC", "trainer1", []string{"bleu"}, true, nil, false, false, false)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.VoteState != models.VoteStateIdle {
		t.Errorf("expected idle state")
	}
	if session.ActiveColors[0] != "bleu" {
		t.Errorf("expected bleu color")
	}
	if !session.MultipleChoice {
		t.Errorf("expected multiple choice true")
	}

	// Unauthorized
	err = m.ResetVote("ABC", "imposter", []string{}, false, nil, false, false, false)
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized")
	}
}

func TestUpdateStagiaireName(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean", "")

	err := m.UpdateStagiaireName("ABC", "s1abc1234567", "Paul")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.Stagiaires["s1abc1234567"] != "Paul" {
		t.Errorf("expected name Paul")
	}

	// Invalid Name
	err = m.UpdateStagiaireName("ABC", "s1abc1234567", "")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput")
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	// Manually set last activity to past
	session, _ := m.GetSession("ABC")
	session.LastActivity = time.Now().Add(-2 * time.Hour).Unix()

	m.CleanupExpiredSessions(time.Hour, nil)

	if _, ok := m.GetSession("ABC"); ok {
		t.Error("Session should have been cleaned up")
	}
}

func TestRemoveSession(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.RemoveSession("ABC")
	if _, ok := m.GetSession("ABC"); ok {
		t.Error("Session should be removed")
	}
}

func TestUpdateTrainer(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	err := m.UpdateTrainer("ABC", "trainer2")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.TrainerID != "trainer2" {
		t.Errorf("expected trainer ID trainer2, got %s", session.TrainerID)
	}

	// Session not found
	err = m.UpdateTrainer("KQR", "trainer2")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestRevealAnswersScoring(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.JoinStagiaire("ABC", "s2abc1234567", "Bob", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, true, false)

	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.SubmitVote("ABC", "s2abc1234567", []string{"bleu"})
	m.CloseVote("ABC", "trainer1")

	entries, err := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})
	if err != nil {
		t.Fatalf("RevealAnswers: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	var alice, bob ScoreEntry
	for _, e := range entries {
		switch e.Name {
		case "Alice":
			alice = e
		case "Bob":
			bob = e
		}
	}
	if alice.VoteScore != PointsPerCorrect {
		t.Errorf("Alice: expected %d, got %d", PointsPerCorrect, alice.VoteScore)
	}
	if bob.VoteScore != PointsPerWrong {
		t.Errorf("Bob: expected %d, got %d", PointsPerWrong, bob.VoteScore)
	}
	if alice.Rank != 1 {
		t.Errorf("Alice should be rank 1, got %d", alice.Rank)
	}
	if bob.Rank != 2 {
		t.Errorf("Bob should be rank 2, got %d", bob.Rank)
	}
}

func TestRevealAnswersIdempotent(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.CloseVote("ABC", "trainer1")

	m.RevealAnswers("ABC", "trainer1", []string{"rouge"})
	entries, _ := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})

	if entries[0].TotalScore != PointsPerCorrect {
		t.Errorf("double reveal should not double score: got %d, expected %d", entries[0].TotalScore, PointsPerCorrect)
	}

	session, _ := m.GetSession("ABC")
	if session.Scores["s1abc1234567"] != PointsPerCorrect {
		t.Errorf("cumulative score should be %d, got %d", PointsPerCorrect, session.Scores["s1abc1234567"])
	}
}

func TestRevealAnswersCorrectsOnChange(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.CloseVote("ABC", "trainer1")

	m.RevealAnswers("ABC", "trainer1", []string{"bleu"})
	entries, _ := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})

	if entries[0].TotalScore != PointsPerCorrect {
		t.Errorf("re-reveal with changed colors should reflect latest: got %d, expected %d", entries[0].TotalScore, PointsPerCorrect)
	}
}

func TestRevealAnswersCumulativeAcrossVotes(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.CloseVote("ABC", "trainer1")
	m.RevealAnswers("ABC", "trainer1", []string{"rouge"})

	m.StartVote("ABC", "trainer1", []string{"bleu"}, false, nil, false, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"bleu"})
	m.CloseVote("ABC", "trainer1")
	entries, _ := m.RevealAnswers("ABC", "trainer1", []string{"bleu"})

	expected := PointsPerCorrect * 2
	if entries[0].TotalScore != expected {
		t.Errorf("cumulative after 2 votes should be %d, got %d", expected, entries[0].TotalScore)
	}
}

func TestRevealAnswersNotClosed(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, true, false)

	_, err := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})
	if err == nil {
		t.Error("expected error when revealing on active vote")
	}
}

func TestRevealAnswersUnauthorized(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, true, false)
	m.CloseVote("ABC", "trainer1")

	_, err := m.RevealAnswers("ABC", "imposter", []string{"rouge"})
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestRevealAnswersWithGameScore(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, true, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})

	m.UpdateGameScore("ABC", "s1abc1234567", 500)
	m.CloseVote("ABC", "trainer1")
	entries, _ := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})

	expected := PointsPerCorrect + 500
	if entries[0].TotalScore != expected {
		t.Errorf("total should include game score: got %d, expected %d", entries[0].TotalScore, expected)
	}
}

func TestSubmitVoteBlank(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, false, true)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"blank"})
	if err != nil {
		t.Errorf("blank vote should succeed when allowed: %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.Votes["s1abc1234567"][0] != "blank" {
		t.Errorf("expected blank vote stored")
	}
}

func TestSubmitVoteBlankNotAllowed(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"blank"})
	if err == nil {
		t.Error("blank vote should fail when not allowed")
	}
}

func TestSubmitVoteBlankWithColors(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, true)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"blank", "rouge"})
	if err == nil {
		t.Error("blank vote combined with colors should fail")
	}
}

func TestUpdateGameScoreMonotonic(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")

	m.UpdateGameScore("ABC", "s1abc1234567", 500)
	m.UpdateGameScore("ABC", "s1abc1234567", 300)

	session, _ := m.GetSession("ABC")
	if session.GameScores["s1abc1234567"] != 500 {
		t.Errorf("game score should be monotonic (keep 500), got %d", session.GameScores["s1abc1234567"])
	}
}

func TestUpdateGameScoreNonexistentStagiaire(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	err := m.UpdateGameScore("ABC", "ghost1234567", 100)
	if err == nil {
		t.Error("expected error for nonexistent stagiaire")
	}
}

func TestRevealAnswersScoreWithBlank(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.JoinStagiaire("ABC", "s2abc1234567", "Bob", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, true, true)

	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.SubmitVote("ABC", "s2abc1234567", []string{"blank"})
	m.CloseVote("ABC", "trainer1")

	entries, err := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})
	if err != nil {
		t.Fatalf("RevealAnswers: %v", err)
	}
	for _, e := range entries {
		if e.Name == "Bob" {
			if e.VoteScore != 0 {
				t.Errorf("blank vote should score 0, got %d", e.VoteScore)
			}
		}
	}
}

func TestStartVoteClearsRevealState(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.CloseVote("ABC", "trainer1")
	m.RevealAnswers("ABC", "trainer1", []string{"rouge"})

	session, _ := m.GetSession("ABC")
	if !session.Revealed {
		t.Fatal("expected Revealed=true after reveal")
	}

	m.StartVote("ABC", "trainer1", []string{"bleu"}, false, nil, false, true, false)
	session, _ = m.GetSession("ABC")
	if session.Revealed {
		t.Error("StartVote should clear Revealed flag")
	}
	if len(session.LastVoteScores) != 0 {
		t.Error("StartVote should clear LastVoteScores")
	}
}

// TestStartVoteRejectsActive covers BM2: a second StartVote while a vote
// is already Active must be rejected so in-progress votes are not
// silently wiped. Closed → StartVote (the legitimate "next round" path)
// must still work.
func TestStartVoteRejectsActive(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	if err := m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false); err != nil {
		t.Fatalf("first StartVote: %v", err)
	}
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})

	// A second StartVote while Active must fail and must NOT clear votes.
	err := m.StartVote("ABC", "trainer1", []string{"bleu"}, false, nil, false, false, false)
	if !errors.Is(err, ErrVoteAlreadyActive) {
		t.Errorf("expected ErrVoteAlreadyActive, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.VoteState != models.VoteStateActive {
		t.Errorf("state should still be Active, got %s", session.VoteState)
	}
	if vote, ok := session.Votes["s1abc1234567"]; !ok || vote[0] != "rouge" {
		t.Errorf("in-progress vote should be preserved, got %v", session.Votes)
	}
	if len(session.ActiveColors) != 1 || session.ActiveColors[0] != "rouge" {
		t.Errorf("ActiveColors should be unchanged, got %v", session.ActiveColors)
	}

	// Close → StartVote is the legitimate next-round path and must work.
	if err := m.CloseVote("ABC", "trainer1"); err != nil {
		t.Fatalf("CloseVote: %v", err)
	}
	if err := m.StartVote("ABC", "trainer1", []string{"bleu"}, false, nil, false, false, false); err != nil {
		t.Errorf("StartVote after Close should succeed, got %v", err)
	}
}

// TestSubmitVoteRejectsDuplicates covers BL6: the duplicate-color check
// lives inside SubmitVote (under the session lock), not only in the hub
// handler. A caller that bypasses the handler still can't stuff a
// ballot with repeated colors.
func TestSubmitVoteRejectsDuplicates(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, true, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge", "rouge"})
	if err == nil {
		t.Fatal("expected error for duplicate colors inside SubmitVote")
	}
	if err.Error() != "duplicate colors are not allowed" {
		t.Errorf("unexpected error message: %v", err)
	}

	// State is unchanged — the rejected vote was not stored.
	session, _ := m.GetSession("ABC")
	if _, ok := session.Votes["s1abc1234567"]; ok {
		t.Error("rejected duplicate vote should not be stored")
	}

	// Sanity: a legitimate multi-choice vote still works.
	if _, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge", "bleu"}); err != nil {
		t.Errorf("legitimate multi-choice vote should succeed, got %v", err)
	}
}

// TestRevealAnswersCompetitionRankTies covers BL2: tied TotalScores
// share a rank (competition ranking), matching what computeRank in hub
// produces on reconnect. Previously RevealAnswers assigned ordinal
// ranks (i+1), so a student tied for first could see "2e" at reveal
// then "1er" after a reconnect.
func TestRevealAnswersCompetitionRankTies(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.JoinStagiaire("ABC", "s2abc1234567", "Bob", "")
	m.JoinStagiaire("ABC", "s3abc1234567", "Carol", "")

	// Alice and Bob both pick the correct color → tie at the top.
	// Carol picks wrong.
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, true, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.SubmitVote("ABC", "s2abc1234567", []string{"rouge"})
	m.SubmitVote("ABC", "s3abc1234567", []string{"bleu"})
	m.CloseVote("ABC", "trainer1")

	entries, err := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})
	if err != nil {
		t.Fatalf("RevealAnswers: %v", err)
	}

	byName := make(map[string]ScoreEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	if byName["Alice"].Rank != 1 || byName["Bob"].Rank != 1 {
		t.Errorf("tied leaders should both be rank 1, got Alice=%d Bob=%d",
			byName["Alice"].Rank, byName["Bob"].Rank)
	}
	if byName["Carol"].Rank != 3 {
		t.Errorf("third place after a tie should be rank 3 (skipped 2), got %d", byName["Carol"].Rank)
	}

	// Sanity: rank values form a competition-ranking sequence — ties
	// share a value, the next distinct score skips. The hub's
	// computeRank (used on reconnect) produces the same invariant.
	ranks := make(map[int]int)
	for _, e := range entries {
		ranks[e.Rank]++
	}
	if ranks[1] != 2 {
		t.Errorf("expected rank 1 shared by 2 entries, got %d", ranks[1])
	}
	if _, ok := ranks[2]; ok {
		t.Errorf("rank 2 must be skipped after a tie at the top, got %d entries", ranks[2])
	}
	if ranks[3] != 1 {
		t.Errorf("expected rank 3 occupied by exactly 1 entry, got %d", ranks[3])
	}
}

// TestRevealAnswersNonCompetitiveDoesNotScore covers BL3: scoring
// (cumulative accumulation, idempotent reversal, the Revealed flag) is
// scoped to Competitive mode by design. A non-competitive reveal still
// returns per-round VoteScore for correctness display but must not
// mutate session.Scores / LastVoteScores / Revealed.
func TestRevealAnswersNonCompetitiveDoesNotScore(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, false, nil, false, false, false)
	m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"})
	m.CloseVote("ABC", "trainer1")

	entries, err := m.RevealAnswers("ABC", "trainer1", []string{"rouge"})
	if err != nil {
		t.Fatalf("RevealAnswers: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].VoteScore != PointsPerCorrect {
		t.Errorf("per-round VoteScore should still be computed: got %d, want %d",
			entries[0].VoteScore, PointsPerCorrect)
	}

	session, _ := m.GetSession("ABC")
	if session.Revealed {
		t.Error("non-competitive reveal must not set Revealed (BL3)")
	}
	if session.Scores["s1abc1234567"] != 0 {
		t.Errorf("non-competitive reveal must not mutate Scores, got %d",
			session.Scores["s1abc1234567"])
	}
	if session.LastVoteScores["s1abc1234567"] != 0 {
		t.Errorf("non-competitive reveal must not mutate LastVoteScores, got %d",
			session.LastVoteScores["s1abc1234567"])
	}

	// A second reveal must also be a no-op (no spurious reversal of a
	// score that was never applied).
	if _, err := m.RevealAnswers("ABC", "trainer1", []string{"bleu"}); err != nil {
		t.Fatalf("second RevealAnswers: %v", err)
	}
	session, _ = m.GetSession("ABC")
	if session.Scores["s1abc1234567"] != 0 {
		t.Errorf("second non-competitive reveal must still not mutate Scores, got %d",
			session.Scores["s1abc1234567"])
	}
}

// TestVotesPerSessionLifetime covers BM4: the VotesPerSession histogram
// must observe the lifetime vote count, not len(Votes) which is reset
// on every StartVote and only reflects the current/last round.
func TestVotesPerSessionLifetime(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice", "")
	m.JoinStagiaire("ABC", "s2abc1234567", "Bob", "")

	// Three rounds, two votes each → lifetime = 6 vote events. At
	// teardown len(Votes) is 2 (the last round); the histogram must
	// record 6, not 2.
	for round := 0; round < 3; round++ {
		colors := []string{"rouge"}
		if err := m.StartVote("ABC", "trainer1", colors, false, nil, false, false, false); err != nil {
			t.Fatalf("StartVote round %d: %v", round, err)
		}
		if _, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge"}); err != nil {
			t.Fatalf("SubmitVote round %d: %v", round, err)
		}
		if _, err := m.SubmitVote("ABC", "s2abc1234567", []string{"rouge"}); err != nil {
			t.Fatalf("SubmitVote round %d: %v", round, err)
		}
		if err := m.CloseVote("ABC", "trainer1"); err != nil {
			t.Fatalf("CloseVote round %d: %v", round, err)
		}
	}

	session, _ := m.GetSession("ABC")
	if session.TotalVotes != 6 {
		t.Errorf("TotalVotes: expected 6, got %d", session.TotalVotes)
	}
	if len(session.Votes) != 2 {
		t.Errorf("len(Votes) sanity check: expected 2 (last round only), got %d", len(session.Votes))
	}

	before := m.Stats().Snapshot().VotesPerSession.Count
	le5Before := func() int64 {
		for _, b := range m.Stats().Snapshot().VotesPerSession.Buckets {
			if b.LE == 5 {
				return b.Count
			}
		}
		return -1
	}()
	m.RemoveSession("ABC")
	after := m.Stats().Snapshot().VotesPerSession.Count
	if after != before+1 {
		t.Fatalf("VotesPerSession.Count: expected +1 sample, got %d → %d", before, after)
	}

	// Find the bucket where the sample landed. With lifetime=6 and
	// buckets {0,1,2,3,5,10,20,50}, the sample lands in le=10 (and le=20,
	// le=50 due to cumulative buckets) but NOT in le=5.
	snap := m.Stats().Snapshot().VotesPerSession
	bucket := func(le float64) int64 {
		for _, b := range snap.Buckets {
			if b.LE == le {
				return b.Count
			}
		}
		return -1
	}
	if bucket(10) < 1 {
		t.Errorf("expected ≥1 sample in le=10 bucket for lifetime=6 votes, got %d", bucket(10))
	}
	// The pre-existing baseline (other tests in this package may also
	// observe into the histogram) is captured *before* our removal, so
	// the le=5 bucket delta must be exactly zero: a 6-vote lifetime
	// must never be recorded as ≤5 (BM4 regression check).
	if delta := bucket(5) - le5Before; delta != 0 {
		t.Errorf("lifetime=6 sample must not land in le=5 bucket, delta=%d", delta)
	}
}
