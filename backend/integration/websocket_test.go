package integration

import (
	"testing"
	"time"

	"vote-backend/internal/models"
)

// TestWebSocketTrainerJoin tests the trainer joining flow.
func TestWebSocketTrainerJoin(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	client := NewWSClient(t, ts.WebSocketURL())
	defer client.Close()

	// Send trainer_join message
	client.SendMessage(TrainerJoin("1234").Build())

	// Should receive session_created
	msg := client.WaitForType("session_created", 2*time.Second)
	if sessionCode, ok := msg["sessionCode"].(string); !ok || sessionCode != "1234" {
		t.Errorf("Expected sessionCode=1234, got %v", msg["sessionCode"])
	}
	// Verify server-generated trainerId is returned
	if _, ok := msg["trainerId"].(string); !ok {
		t.Errorf("Expected trainerId in session_created response")
	}

	// Should also receive connected_count (0 stagiaires)
	msg = client.ReceiveMessage(500 * time.Millisecond)
	if msg["type"] != "connected_count" {
		t.Errorf("Expected connected_count, got %v", msg["type"])
	}
	if count, ok := msg["count"].(float64); !ok || count != 0 {
		t.Errorf("Expected count=0, got %v", msg["count"])
	}

	// Verify session exists in hub
	if !ts.Hub().SessionExists("1234") {
		t.Error("Session should exist in hub")
	}
}

// TestWebSocketStagiaireJoin tests the stagiaire joining flow.
func TestWebSocketStagiaireJoin(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// First, create a session with trainer
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("5678").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	// Drain connected_count
	trainer.ReceiveMessage(500 * time.Millisecond)

	// Now stagiaire joins (no longer sending stagiaireId - it's server-generated)
	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin("5678", "", "Alice").Build())

	// Stagiaire receives session_joined
	msg := stagiaire.WaitForType("session_joined", 2*time.Second)
	if sessionCode, ok := msg["sessionCode"].(string); !ok || sessionCode != "5678" {
		t.Errorf("Expected sessionCode=5678, got %v", msg["sessionCode"])
	}
	// Verify server-generated stagiaireId is returned (12 chars)
	if stagiaireID, ok := msg["stagiaireId"].(string); !ok || len(stagiaireID) != 12 {
		t.Errorf("Expected 12-char stagiaireId in session_joined, got %v", msg["stagiaireId"])
	}

	// Trainer receives connected_count update
	msg = trainer.WaitForType("connected_count", 2*time.Second)
	if count, ok := msg["count"].(float64); !ok || count != 1 {
		t.Errorf("Expected count=1, got %v", msg["count"])
	}
}

// TestWebSocketVoteFlow tests the complete voting flow.
func TestWebSocketVoteFlow(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Setup trainer and session
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("9999").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	trainer.ReceiveMessage(500 * time.Millisecond) // connected_count

	// Stagiaire joins (ID is now server-generated)
	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin("9999", "", "Bob").Build())

	// Capture the server-generated stagiaireId
	joinMsg := stagiaire.WaitForType("session_joined", 2*time.Second)
	stagiaireID, ok := joinMsg["stagiaireId"].(string)
	if !ok || len(stagiaireID) != 12 {
		t.Fatalf("Expected 12-char stagiaireId in session_joined, got %v", joinMsg["stagiaireId"])
	}

	trainer.WaitForType("connected_count", 2*time.Second)

	// Trainer starts vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu"}, false).Build())

	// Stagiaire receives vote_started
	msg := stagiaire.WaitForType("vote_started", 2*time.Second)
	if colors, ok := msg["colors"].([]interface{}); !ok || len(colors) != 2 {
		t.Errorf("Expected 2 colors, got %v", msg["colors"])
	}

	// Trainer also receives vote_started (for UI consistency)
	trainer.WaitForType("vote_started", 500 * time.Millisecond)

	// Stagiaire votes
	stagiaire.SendMessage(Vote("rouge").Build())

	// Stagiaire receives vote_accepted (no colors field in actual protocol)
	msg = stagiaire.WaitForType("vote_accepted", 2*time.Second)
	if msg["type"] != "vote_accepted" {
		t.Errorf("Expected vote_accepted, got %v", msg["type"])
	}

	// Trainer receives vote_received with the server-generated stagiaireId
	msg = trainer.WaitForType("vote_received", 2*time.Second)
	if receivedID, ok := msg["stagiaireId"].(string); !ok || receivedID != stagiaireID {
		t.Errorf("Expected stagiaireId=%s, got %v", stagiaireID, msg["stagiaireId"])
	}
	if colors, ok := msg["colors"].([]interface{}); !ok || len(colors) != 1 {
		t.Errorf("Expected 1 color in vote_received, got %v", msg["colors"])
	}

	// Trainer closes vote
	trainer.SendMessage(NewCloseVote().Build())

	// Stagiaire receives vote_closed
	msg = stagiaire.WaitForType("vote_closed", 2*time.Second)
	if msg["type"] != "vote_closed" {
		t.Errorf("Expected vote_closed, got %v", msg["type"])
	}
}

// TestWebSocketMultipleChoice tests multiple choice voting.
func TestWebSocketMultipleChoice(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Setup trainer and session
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("1111").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	trainer.ReceiveMessage(500 * time.Millisecond) // connected_count

	// Stagiaire joins
	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin("1111", "", "Carol").Build())
	stagiaire.WaitForType("session_joined", 2*time.Second)
	trainer.WaitForType("connected_count", 2*time.Second)

	// Trainer starts multiple choice vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu", "vert"}, true).Build())

	stagiaire.WaitForType("vote_started", 2*time.Second)
	trainer.WaitForType("vote_started", 500 * time.Millisecond)

	// Stagiaire votes for multiple colors
	stagiaire.SendMessage(Vote("rouge", "bleu").Build())

	// Stagiaire receives vote_accepted (no colors in vote_accepted)
	msg := stagiaire.WaitForType("vote_accepted", 2*time.Second)
	if msg["type"] != "vote_accepted" {
		t.Errorf("Expected vote_accepted, got %v", msg["type"])
	}

	// Trainer receives vote_received with both colors
	msg = trainer.WaitForType("vote_received", 2*time.Second)
	colors, ok := msg["colors"].([]interface{})
	if !ok || len(colors) != 2 {
		t.Errorf("Expected 2 colors in vote_received, got %v", msg["colors"])
	}
}

// TestWebSocketResetVote tests the vote reset functionality.
func TestWebSocketResetVote(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Setup
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("2222").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	trainer.ReceiveMessage(500 * time.Millisecond)

	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin("2222", "", "Dave").Build())
	stagiaire.WaitForType("session_joined", 2*time.Second)
	trainer.WaitForType("connected_count", 2*time.Second)

	// Start and cast a vote
	trainer.SendMessage(StartVote([]string{"rouge"}, false).Build())
	stagiaire.WaitForType("vote_started", 2*time.Second)
	trainer.WaitForType("vote_started", 500 * time.Millisecond)

	stagiaire.SendMessage(Vote("rouge").Build())
	stagiaire.WaitForType("vote_accepted", 2*time.Second)
	trainer.WaitForType("vote_received", 2*time.Second)

	// Reset vote with new colors
	trainer.SendMessage(ResetVote([]string{"bleu", "vert"}, false).Build())

	// Stagiaire receives vote_reset (no vote_started automatically)
	msg := stagiaire.WaitForType("vote_reset", 2*time.Second)
	if msg["type"] != "vote_reset" {
		t.Errorf("Expected vote_reset, got %v", msg["type"])
	}

	// Vote needs to be started again with start_vote
	trainer.SendMessage(StartVote([]string{"bleu", "vert"}, false).Build())
	msg = stagiaire.WaitForType("vote_started", 2*time.Second)
	colors, ok := msg["colors"].([]interface{})
	if !ok || len(colors) != 2 {
		t.Errorf("Expected 2 colors in new vote_started, got %v", msg["colors"])
	}
}

// TestWebSocketNameUpdate tests the name update functionality.
func TestWebSocketNameUpdate(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Setup
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("3333").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	trainer.ReceiveMessage(500 * time.Millisecond)

	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin("3333", "", "Eve").Build())
	stagiaire.WaitForType("session_joined", 2*time.Second)
	trainer.WaitForType("connected_count", 2*time.Second)

	// Update name
	stagiaire.SendMessage(UpdateName("Evelyn").Build())

	// Stagiaire receives name_updated
	msg := stagiaire.WaitForType("name_updated", 2*time.Second)
	if name, ok := msg["name"].(string); !ok || name != "Evelyn" {
		t.Errorf("Expected name=Evelyn, got %v", msg["name"])
	}

	// Trainer receives stagiaire_names_updated
	msg = trainer.WaitForType("stagiaire_names_updated", 2*time.Second)
	stagiaires, ok := msg["stagiaires"].([]interface{})
	if !ok || len(stagiaires) != 1 {
		t.Errorf("Expected 1 stagiaire in names_updated, got %v", msg["stagiaires"])
	}
}

// TestWebSocketInvalidSession tests error handling for invalid session codes.
func TestWebSocketInvalidSession(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Try to join non-existent session as stagiaire
	stagiaire := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire.Close()
	stagiaire.SendMessage(StagiaireJoin("0000", "", "Frank").Build())

	// Should receive error
	msg := stagiaire.WaitForType("error", 2*time.Second)
	if msg["type"] != "error" {
		t.Errorf("Expected error, got %v", msg["type"])
	}
	// backoffMs should NOT be present (security fix - no timing disclosure)
	if _, ok := msg["backoffMs"]; ok {
		t.Error("backoffMs should not be present in error response (security fix)")
	}
}

// TestWebSocketMalformedMessage tests handling of malformed messages.
func TestWebSocketMalformedMessage(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	client := NewWSClient(t, ts.WebSocketURL())
	defer client.Close()

	// Send invalid JSON directly via send channel
	select {
	case client.send <- []byte("{invalid json}"):
	case <-time.After(time.Second):
		t.Fatal("Failed to send message")
	}

	// Connection should remain open (server logs error but doesn't crash)
	// Send a valid message to verify
	client.SendMessage(TrainerJoin("4444").Build())
	msg := client.WaitForType("session_created", 2*time.Second)
	if msg["type"] != "session_created" {
		t.Errorf("Expected session_created, got %v", msg["type"])
	}
}

// TestWebSocketUnknownMessageType tests handling of unknown message types.
func TestWebSocketUnknownMessageType(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	client := NewWSClient(t, ts.WebSocketURL())
	defer client.Close()

	// Send unknown message type
	client.SendMessage(models.Message{Type: "unknown_type"})

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Connection should still work
	client.SendMessage(TrainerJoin("5555").Build())
	msg := client.WaitForType("session_created", 2*time.Second)
	if msg["type"] != "session_created" {
		t.Errorf("Expected session_created, got %v", msg["type"])
	}
}

// TestWebSocketMultipleStagiaires tests multiple stagiaires in the same session.
func TestWebSocketMultipleStagiaires(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Setup trainer
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("6666").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	trainer.ReceiveMessage(500 * time.Millisecond)

	// Create multiple stagiaires
	numStagiaires := 5
	stagiaires := make([]*WSClient, numStagiaires)
	for i := 0; i < numStagiaires; i++ {
		s := NewWSClient(t, ts.WebSocketURL())
		stagiaires[i] = s
		defer s.Close()
		s.SendMessage(StagiaireJoin("6666", "", "Stagiaire"+string(rune('1'+i))).Build())
		s.WaitForType("session_joined", 2*time.Second)
	}

	// Trainer should receive connected_count for each
	for i := 0; i < numStagiaires; i++ {
		msg := trainer.WaitForType("connected_count", 2*time.Second)
		if count, ok := msg["count"].(float64); !ok || int(count) != i+1 {
			t.Errorf("Expected count=%d, got %v", i+1, msg["count"])
		}
	}

	// Start vote
	trainer.SendMessage(StartVote([]string{"rouge", "bleu"}, false).Build())
	trainer.WaitForType("vote_started", 2*time.Second)

	// All stagiaires should receive vote_started
	for _, s := range stagiaires {
		s.WaitForType("vote_started", 2*time.Second)
	}

	// Each stagiaire votes
	for i, s := range stagiaires {
		color := "rouge"
		if i%2 == 0 {
			color = "bleu"
		}
		s.SendMessage(Vote(color).Build())
		s.WaitForType("vote_accepted", 2*time.Second)
	}

	// Trainer should receive all vote_received messages
	for i := 0; i < numStagiaires; i++ {
		msg := trainer.WaitForType("vote_received", 2*time.Second)
		if msg["type"] != "vote_received" {
			t.Errorf("Expected vote_received %d, got %v", i+1, msg["type"])
		}
	}
}

// TestWebSocketReconnection tests client reconnection scenario.
func TestWebSocketReconnection(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Create session with trainer
	trainer := NewWSClient(t, ts.WebSocketURL())
	defer trainer.Close()
	trainer.SendMessage(TrainerJoin("7777").Build())
	trainer.WaitForType("session_created", 2*time.Second)
	trainer.ReceiveMessage(500 * time.Millisecond)

	// Stagiaire joins and votes (ID is server-generated)
	stagiaire1 := NewWSClient(t, ts.WebSocketURL())
	stagiaire1.SendMessage(StagiaireJoin("7777", "", "Grace").Build())
	msg := stagiaire1.WaitForType("session_joined", 2*time.Second)
	if msg["type"] != "session_joined" {
		t.Fatalf("Expected session_joined, got %v", msg["type"])
	}
	// Capture the server-generated ID
	stagiaireID, ok := msg["stagiaireId"].(string)
	if !ok || len(stagiaireID) != 12 {
		t.Fatalf("Expected 12-char stagiaireId, got %v", msg["stagiaireId"])
	}
	trainer.WaitForType("connected_count", 2*time.Second)

	// Start vote and cast vote
	trainer.SendMessage(StartVote([]string{"rouge"}, false).Build())
	stagiaire1.WaitForType("vote_started", 2*time.Second)
	trainer.WaitForType("vote_started", 500 * time.Millisecond)

	stagiaire1.SendMessage(Vote("rouge").Build())
	stagiaire1.WaitForType("vote_accepted", 2*time.Second)
	trainer.WaitForType("vote_received", 2*time.Second)

	// Close first connection
	stagiaire1.Close()

	// Wait for disconnect to be processed
	time.Sleep(200 * time.Millisecond)

	// Drain any pending messages from trainer
	trainer.DrainMessages()

	// Reconnect - the server will assign a new ID, but the name "Grace" will be preserved
	stagiaire2 := NewWSClient(t, ts.WebSocketURL())
	defer stagiaire2.Close()
	stagiaire2.SendMessage(StagiaireJoin("7777", "", "Grace").Build())
	msg = stagiaire2.WaitForType("session_joined", 2*time.Second)
	if msg["type"] != "session_joined" {
		t.Errorf("Expected session_joined after reconnect, got %v", msg["type"])
	}

	// Trainer should receive connected_count
	msg = trainer.WaitForType("connected_count", 2*time.Second)
	if count, ok := msg["count"].(float64); !ok || count != 1 {
		t.Errorf("Expected count=1 after reconnect, got %v", msg["count"])
	}
}
