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
	session, err := m.CreateSession("1234", "trainer1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if session == nil {
		t.Fatal("expected session to be returned")
	}
	if session.ID != "1234" {
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
	m.CreateSession("1234", "trainer1")

	// Valid join - use exactly 12-char lowercase alphanumeric ID matching GenerateID format
	err := m.JoinStagiaire("1234", "stag1ab12cde", "Jean")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("1234")
	if session.Stagiaires["stag1ab12cde"] != "Jean" {
		t.Errorf("expected name Jean, got %s", session.Stagiaires["stag1ab12cde"])
	}

	// Invalid session
	err = m.JoinStagiaire("9999", "stag1ab12cde", "Jean")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	// Invalid Name
	err = m.JoinStagiaire("1234", "stag1ab12cde", "<script>")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestStartVote(t *testing.T) {
	m := NewManager()
	m.CreateSession("1234", "trainer1")

	colors := []string{"rouge", "bleu"}
	err := m.StartVote("1234", "trainer1", colors, true)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("1234")
	if session.VoteState != models.VoteStateActive {
		t.Errorf("expected active state, got %s", session.VoteState)
	}
	if len(session.ActiveColors) != 2 {
		t.Errorf("expected 2 active colors")
	}

	// Unauthorized trainer
	err = m.StartVote("1234", "imposter", colors, true)
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestSubmitVote(t *testing.T) {
	m := NewManager()
	m.CreateSession("1234", "trainer1")
	m.JoinStagiaire("1234", "s1abc1234567", "Jean")
	m.StartVote("1234", "trainer1", []string{"rouge", "bleu"}, false)

	// Valid vote
	name, err := m.SubmitVote("1234", "s1abc1234567", []string{"rouge"})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if name != "Jean" {
		t.Errorf("expected name Jean, got %s", name)
	}

	session, _ := m.GetSession("1234")
	if session.Votes["s1abc1234567"][0] != "rouge" {
		t.Errorf("expected vote rouge")
	}

	// Invalid color
	_, err = m.SubmitVote("1234", "s1abc1234567", []string{"vert"})
	if err == nil {
		t.Error("expected error for invalid color")
	}

	// Vote when not active
	m.CloseVote("1234", "trainer1")
	_, err = m.SubmitVote("1234", "s1abc1234567", []string{"rouge"})
	if err == nil {
		t.Error("expected error when vote closed")
	}
}

func TestResetVote(t *testing.T) {
	m := NewManager()
	m.CreateSession("1234", "trainer1")
	m.StartVote("1234", "trainer1", []string{"rouge"}, false)

	err := m.ResetVote("1234", "trainer1", []string{"bleu"}, true)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("1234")
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
	err = m.ResetVote("1234", "imposter", []string{}, false)
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized")
	}
}

func TestUpdateStagiaireName(t *testing.T) {
	m := NewManager()
	m.CreateSession("1234", "trainer1")
	m.JoinStagiaire("1234", "s1abc1234567", "Jean")

	err := m.UpdateStagiaireName("1234", "s1abc1234567", "Paul")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("1234")
	if session.Stagiaires["s1abc1234567"] != "Paul" {
		t.Errorf("expected name Paul")
	}

	// Invalid Name
	err = m.UpdateStagiaireName("1234", "s1abc1234567", "")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput")
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	m := NewManager()
	m.CreateSession("1234", "trainer1")

	// Manually set last activity to past
	session, _ := m.GetSession("1234")
	session.LastActivity = time.Now().Add(-2 * time.Hour).Unix()

	m.CleanupExpiredSessions(time.Hour)

	if _, ok := m.GetSession("1234"); ok {
		t.Error("Session should have been cleaned up")
	}
}

func TestRemoveSession(t *testing.T) {
    m := NewManager()
    m.CreateSession("1234", "trainer1")
    m.RemoveSession("1234")
    if _, ok := m.GetSession("1234"); ok {
        t.Error("Session should be removed")
    }
}

func TestUpdateTrainer(t *testing.T) {
	m := NewManager()
	m.CreateSession("1234", "trainer1")

	err := m.UpdateTrainer("1234", "trainer2")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	session, _ := m.GetSession("1234")
	if session.TrainerID != "trainer2" {
		t.Errorf("expected trainer ID trainer2, got %s", session.TrainerID)
	}

	// Session not found
	err = m.UpdateTrainer("9999", "trainer2")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}
