package vote

import (
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

func TestJoinStagiaire(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")

	// Valid join - use exactly 12-char lowercase alphanumeric ID matching GenerateID format
	err := m.JoinStagiaire("ABC", "stag1ab12cde", "Jean")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("ABC")
	if session.Stagiaires["stag1ab12cde"] != "Jean" {
		t.Errorf("expected name Jean, got %s", session.Stagiaires["stag1ab12cde"])
	}

	// Invalid session
	err = m.JoinStagiaire("KQR", "stag1ab12cde", "Jean")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	// Invalid Name
	err = m.JoinStagiaire("ABC", "stag1ab12cde", "<script>")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean")
	m.StartVote("ABC", "trainer1", []string{"rouge", "bleu"}, true, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"rouge", "bleu"})
	if err != nil {
		t.Errorf("multiple colors should be allowed in multiple-choice mode, got: %v", err)
	}
}

func TestSubmitVoteEmptyColors(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Jean")

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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
	m.JoinStagiaire("ABC", "s2abc1234567", "Bob")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"blank"})
	if err == nil {
		t.Error("blank vote should fail when not allowed")
	}
}

func TestSubmitVoteBlankWithColors(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
	m.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, true)

	_, err := m.SubmitVote("ABC", "s1abc1234567", []string{"blank", "rouge"})
	if err == nil {
		t.Error("blank vote combined with colors should fail")
	}
}

func TestUpdateGameScoreMonotonic(t *testing.T) {
	m := NewManager()
	m.CreateSession("ABC", "trainer1")
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")

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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
	m.JoinStagiaire("ABC", "s2abc1234567", "Bob")
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
	m.JoinStagiaire("ABC", "s1abc1234567", "Alice")
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
