package integration

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/server"
)

// TestScenarioCompleteVotingSession tests a complete voting session from start to finish.
func TestScenarioCompleteVotingSession(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Step 1: Trainer creates session (server generates code)
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("").Build()) // Empty triggers server generation

	msg := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode, ok := msg["sessionCode"].(string)
	if !ok || len(sessionCode) != 4 {
		t.Errorf("Expected 4-digit sessionCode, got %v", msg["sessionCode"])
	}

	// Verify initial connected_count
	msg = trainer.ReceiveMessage(500 * time.Millisecond)
	if count, ok := msg["count"].(float64); !ok || count != 0 {
		t.Errorf("Expected initial count=0, got %v", msg["count"])
	}

	// Step 2: Multiple stagiaires join
	names := []string{"Alice", "Bob", "Charlie", "Diana"}
	stagiaires := make([]*WSClient, len(names))

	for i, name := range names {
		s := NewWSClient(t, ts.WebSocketURL())
		stagiaires[i] = s
		defer s.Close()

		stagiaireID := "s" + string(rune('1'+i))
		s.SendMessage(StagiaireJoin(sessionCode, stagiaireID, name).Build())

		// Verify stagiaire joined
		msg := s.WaitForType("session_joined", 2*time.Second)
		if msg["sessionCode"] != sessionCode {
			t.Errorf("Expected sessionCode=%s, got %v", sessionCode, msg["sessionCode"])
		}

		// Verify trainer got update
		msg = trainer.WaitForType("connected_count", 2*time.Second)
		if count, ok := msg["count"].(float64); !ok || int(count) != i+1 {
			t.Errorf("Expected count=%d, got %v", i+1, msg["count"])
		}
	}

	// Step 3: Trainer starts vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu", "vert"}, false).Build())
	trainer.WaitForType("vote_started", 2*time.Second)

	// All stagiaires receive vote_started
	for _, s := range stagiaires {
		s.WaitForType("vote_started", 2*time.Second)
	}

	// Step 4: Stagiaires cast votes
	votes := []string{"rouge", "bleu", "rouge", "vert"}
	for i, s := range stagiaires {
		s.SendMessage(Vote(votes[i]).Build())
		s.WaitForType("vote_accepted", 2*time.Second)
	}

	// Trainer receives all votes
	receivedVotes := make(map[string]int)
	for i := 0; i < len(stagiaires); i++ {
		msg := trainer.WaitForType("vote_received", 2*time.Second)
		colors, ok := msg["colors"].([]interface{})
		if !ok || len(colors) != 1 {
			t.Errorf("Expected 1 color, got %v", msg["colors"])
			continue
		}
		color := colors[0].(string)
		receivedVotes[color]++
	}

	// Verify vote distribution
	if receivedVotes["rouge"] != 2 {
		t.Errorf("Expected 2 rouge votes, got %d", receivedVotes["rouge"])
	}
	if receivedVotes["bleu"] != 1 {
		t.Errorf("Expected 1 bleu vote, got %d", receivedVotes["bleu"])
	}
	if receivedVotes["vert"] != 1 {
		t.Errorf("Expected 1 vert vote, got %d", receivedVotes["vert"])
	}

	// Step 5: Trainer closes vote
	trainer.SendMessage(NewCloseVote().Build())

	// All stagiaires receive vote_closed
	for _, s := range stagiaires {
		s.WaitForType("vote_closed", 2*time.Second)
	}

	// Step 6: Trainer resets for new vote
	trainer.SendMessage(ResetVote([]string{"jaune", "violet"}, false).Build())

	// All stagiaires receive vote_reset (and possibly vote_started)
	for _, s := range stagiaires {
		s.WaitForType("vote_reset", 2*time.Second)
	}
	trainer.WaitForType("vote_reset", 2*time.Second)

	// Note: vote_started may or may not be sent after reset depending on implementation
	// The key is that vote_reset was received
}

// TestScenarioMultipleChoiceVoting tests a multiple choice voting session.
func TestScenarioMultipleChoiceVoting(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("").Build()) // Server generates code
	msg := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode, _ := msg["sessionCode"].(string)
	trainer.ReceiveMessage(500 * time.Millisecond)

	// Stagiaires join
	stagiaires := make([]*WSClient, 3)
	for i := 0; i < 3; i++ {
		s := NewWSClient(t, ts.WebSocketURL())
		stagiaires[i] = s
		defer s.Close()
		s.SendMessage(StagiaireJoin(sessionCode, "m"+string(rune('1'+i)), "User"+string(rune('1'+i))).Build())
		s.WaitForType("session_joined", 2*time.Second)
		trainer.WaitForType("connected_count", 2*time.Second)
	}

	// Start multiple choice vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu", "vert", "jaune"}, true).Build())
	trainer.WaitForType("vote_started", 2*time.Second)

	for _, s := range stagiaires {
		s.WaitForType("vote_started", 2*time.Second)
	}

	// Stagiaires vote for multiple options
	votePatterns := [][]string{
		{"rouge", "bleu"},
		{"bleu", "vert"},
		{"rouge", "vert", "jaune"},
	}

	for i, s := range stagiaires {
		s.SendMessage(Vote(votePatterns[i]...).Build())
		msg := s.WaitForType("vote_accepted", 2*time.Second)
		if msg["type"] != "vote_accepted" {
			t.Errorf("Stagiaire %d: expected vote_accepted, got %v", i, msg["type"])
		}
	}

	// Trainer receives all multi-choice votes
	for i := 0; i < 3; i++ {
		msg := trainer.WaitForType("vote_received", 2*time.Second)
		colors, ok := msg["colors"].([]interface{})
		if !ok || len(colors) != len(votePatterns[i]) {
			t.Errorf("Trainer: expected %d colors, got %v", len(votePatterns[i]), msg["colors"])
		}
	}
}

// TestScenarioNamePersistence tests that names are stored correctly.
// Note: With server-generated IDs, each connection gets a new ID, so
// name persistence across reconnections with the same ID is no longer
// applicable. This test now verifies that the name is correctly stored.
func TestScenarioNamePersistence(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("").Build()) // Server generates code
	msg := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode, _ := msg["sessionCode"].(string)
	// Drain initial messages but keep the channel clear
	for i := 0; i < 3; i++ {
		trainer.TryReceiveMessage(100 * time.Millisecond)
	}

	// Stagiaire joins with a name (ID is server-generated)
	stagiaire1 := NewWSClient(t, ts.WebSocketURL())
	stagiaire1.SendMessage(StagiaireJoin(sessionCode, "", "OriginalName").Build())
	stagiaire1.WaitForType("session_joined", 2*time.Second)

	// Verify trainer received the name in connected_count
	msg = trainer.WaitForType("connected_count", 2*time.Second)
	stagiaires, ok := msg["stagiaires"].([]interface{})
	if !ok || len(stagiaires) != 1 {
		t.Fatalf("Expected 1 stagiaire in connected_count, got %v", msg["stagiaires"])
	}

	// Verify the name
	s := stagiaires[0].(map[string]interface{})
	if s["name"] != "OriginalName" {
		t.Errorf("Expected name=OriginalName, got %v", s["name"])
	}

	// Verify the ID is 12 chars (server-generated)
	if id, ok := s["id"].(string); !ok || len(id) != 12 {
		t.Errorf("Expected 12-char server-generated ID, got %v", s["id"])
	}

	// Disconnect stagiaire
	stagiaire1.Close()
	time.Sleep(200 * time.Millisecond)

	// Wait for count to go to 0
	msg = trainer.WaitForType("connected_count", 2*time.Second)
	if count, ok := msg["count"].(float64); ok && count != 0 {
		t.Logf("After disconnect, count is %v (expected 0)", count)
	}

	// Reconnect - server generates a new ID
	stagiaire2 := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire2.Close()
	stagiaire2.SendMessage(StagiaireJoin(sessionCode, "", "NewName").Build())
	stagiaire2.WaitForType("session_joined", 2*time.Second)

	// Trainer receives connected_count with the new connection
	msg = trainer.WaitForType("connected_count", 2*time.Second)
	if count, ok := msg["count"].(float64); !ok || count != 1 {
		t.Errorf("Expected count=1, got %v", msg["count"])
	}

	stagiaires = msg["stagiaires"].([]interface{})
	// With server-generated IDs, reconnecting creates a new entry
	// The old entry's name is preserved but disconnected
	// We now have 2 entries in the session's stagiaires map
	if len(stagiaires) < 1 {
		t.Fatalf("Expected at least 1 stagiaire, got %d", len(stagiaires))
	}

	// Find the connected stagiaire (the new one)
	var connectedStagiaire map[string]interface{}
	for _, st := range stagiaires {
		s := st.(map[string]interface{})
		if connected, ok := s["connected"].(bool); ok && connected {
			connectedStagiaire = s
			break
		}
	}

	if connectedStagiaire == nil {
		t.Fatal("Expected to find a connected stagiaire")
	}

	// Verify the new name is used
	if connectedStagiaire["name"] != "NewName" {
		t.Errorf("Expected name=NewName for connected stagiaire, got %v", connectedStagiaire["name"])
	}
}

// TestScenarioErrorRecovery tests that the system recovers from errors.
func TestScenarioErrorRecovery(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("").Build()) // Server generates code
	msg := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode, _ := msg["sessionCode"].(string)
	trainer.ReceiveMessage(500 * time.Millisecond)

	// Try to join non-existent session (use a 4-digit code that doesn't exist)
	invalidClient := NewWSClient(t, ts.WebSocketURL())
	defer invalidClient.Close()
	invalidClient.SendMessage(StagiaireJoin("9999", "bad", "Bad").Build())

	// Should get error
	msg = invalidClient.WaitForType("error", 2*time.Second)
	if msg["type"] != "error" {
		t.Errorf("Expected error message, got %v", msg["type"])
	}

	// Valid join should still work
	validClient := NewWSClient(t, ts.WebSocketURL())
	defer validClient.Close()
	validClient.SendMessage(StagiaireJoin(sessionCode, "good", "Good").Build())
	validClient.WaitForType("session_joined", 2*time.Second)
	trainer.WaitForType("connected_count", 2*time.Second)

	// Vote with invalid color before vote starts
	validClient.SendMessage(Vote("invalid-color").Build())
	// Should get error
	msg = validClient.WaitForType("error", 2*time.Second)
	if msg["type"] != "error" {
		t.Errorf("Expected error for invalid vote, got %v", msg["type"])
	}

	// System should still work - start real vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu"}, false).Build())
	trainer.WaitForType("vote_started", 2*time.Second)
	validClient.WaitForType("vote_started", 2*time.Second)

	// Valid vote should work
	validClient.SendMessage(Vote("rouge").Build())
	validClient.WaitForType("vote_accepted", 2*time.Second)
	trainer.WaitForType("vote_received", 2*time.Second)
}

// TestScenarioConcurrentVotes tests multiple votes arriving.
func TestScenarioConcurrentVotes(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("").Build()) // Server generates code
	msg := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode, _ := msg["sessionCode"].(string)

	// Create 3 stagiaires sequentially
	stagiaires := make([]*WSClient, 3)
	ids := []string{"u1", "u2", "u3"}

	for i, id := range ids {
		s := NewWSClient(t, ts.WebSocketURL())
		stagiaires[i] = s
		defer s.Close()
		s.SendMessage(StagiaireJoin(sessionCode, id, "User"+id).Build())
		s.WaitForType("session_joined", 2*time.Second)
	}

	// Drain connected_count messages
	for i := 0; i < 3; i++ {
		trainer.WaitForType("connected_count", 2*time.Second)
	}

	// Start vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu", "vert"}, false).Build())
	trainer.WaitForType("vote_started", 2*time.Second)

	// All stagiaires receive vote_started
	for _, s := range stagiaires {
		s.WaitForType("vote_started", 2*time.Second)
	}

	// All stagiaires vote (different colors)
	colors := []string{"rouge", "bleu", "vert"}
	for i, s := range stagiaires {
		s.SendMessage(Vote(colors[i]).Build())
		s.WaitForType("vote_accepted", 2*time.Second)
	}

	// Trainer receives all votes
	for i := 0; i < 3; i++ {
		trainer.WaitForType("vote_received", 2*time.Second)
	}
}

// TestScenarioSessionTimeout tests session cleanup after timeout.
func TestScenarioSessionTimeout(t *testing.T) {
	// Use short timeout for testing
	cfg := &config.Config{
		Port:            getFreePort(t),
		SessionTimeout:  500 * time.Millisecond,
		CleanupInterval: 100 * time.Millisecond,
		PingInterval:    30 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 5 * time.Second,
		AllowedOrigins:  []string{"*"},
		ValidColors: []string{
			"rouge", "vert", "bleu", "jaune",
			"orange", "violet", "rose", "gris",
		},
	}

	gin.SetMode(gin.TestMode)

	h := hub.NewHub(cfg)
	go h.Run()

	srv := server.NewServer(cfg, h)

	go func() {
		srv.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	wsURL := "ws://localhost:" + cfg.Port + "/ws"

	// Create a session with server-generated code
	trainer := NewWSClient(t, wsURL)
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("").Build())
	msg := trainer.WaitForType("session_created", 2*time.Second)
	sessionCode, _ := msg["sessionCode"].(string)

	// Session should exist
	if !h.SessionExists(sessionCode) {
		t.Error("Session should exist immediately after creation")
	}

	// Close trainer connection
	trainer.Close()

	// Wait for session timeout (2x cleanup interval + timeout)
	time.Sleep(time.Second)

	// Session should be cleaned up
	if h.SessionExists(sessionCode) {
		t.Error("Session should be cleaned up after timeout")
	}

	h.Shutdown()
	srv.Shutdown(context.Background())
}
