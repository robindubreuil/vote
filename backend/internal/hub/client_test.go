package hub

import (
	"encoding/json"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/models"
)

func initTestHandlers(c *Client) {
	c.handlers = map[string]func(models.Message){
		"trainer_join":   c.handleTrainerJoin,
		"stagiaire_join": c.handleStagiaireJoin,
		"start_vote":     c.handleStartVote,
		"vote":           c.handleVote,
		"close_vote":     c.handleCloseVote,
		"reset_vote":     c.handleResetVote,
		"update_name":    c.handleUpdateName,
	}
}

func TestClientHandleMessage(t *testing.T) {
	// Setup Hub
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second, // Need positive ping interval for NewClient
		ValidColors: []string{
			"rouge", "vert", "bleu", "jaune",
			"orange", "violet", "rose", "gris",
		},
	}
	h := NewHub(cfg)
	go h.Run()
	defer h.Shutdown()

	// 1. Test Trainer Join - use 12-char lowercase alphanumeric ID matching GenerateID format
	trainer := &Client{
		ID:   "trainer1abcde",
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}
	initTestHandlers(trainer)

	joinMsg := models.Message{
		Type:        "trainer_join",
		SessionCode: "1234",
	}
	joinBytes, _ := json.Marshal(joinMsg)
	trainer.handleMessage(joinBytes)

	// Trainer receives 3 messages upon join:
	// 1. connected_count (from registerClient)
	// 2. config_updated (from registerClient)
	// 3. session_created (from handleTrainerJoin)
	// We need to consume all of them to clear the channel for subsequent tests.

	expectedTypes := map[string]bool{
		"connected_count": true,
		"config_updated":  true,
		"session_created": true,
	}

	for i := 0; i < 3; i++ {
		select {
		case msg := <-trainer.Send:
			var resp map[string]interface{}
			json.Unmarshal(msg, &resp)
			msgType := resp["type"].(string)
			if !expectedTypes[msgType] {
				t.Errorf("Unexpected message type during join: %v", msgType)
			}
			delete(expectedTypes, msgType)
		case <-time.After(time.Second):
			t.Error("Timeout waiting for trainer join messages")
		}
	}

	if len(expectedTypes) > 0 {
		t.Errorf("Did not receive all expected messages. Missing: %v", expectedTypes)
	}

	// Wait for hub to process registration
	time.Sleep(50 * time.Millisecond)

	if !h.SessionExists("1234") {
		t.Error("Session 1234 should exist")
	}

	// 2. Test Stagiaire Join - use 12-char lowercase alphanumeric ID matching GenerateID format
	stagiaire := &Client{
		ID:   "s1abc1234567", // Server-generated ID (set in handleWebSocket in real flow)
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}
	initTestHandlers(stagiaire)

	stJoinMsg := models.Message{
		Type:        "stagiaire_join",
		SessionCode: "1234",
		// StagiaireID is no longer used - ID comes from client.ID (server-generated)
		Name:        "Bob",
	}
	stJoinBytes, _ := json.Marshal(stJoinMsg)
	stagiaire.handleMessage(stJoinBytes)

	// Verify stagiaire received session_joined
	select {
	case msg := <-stagiaire.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "session_joined" {
			t.Errorf("Expected session_joined, got %v", resp["type"])
		}
		// Verify the server-generated ID is returned
		if resp["stagiaireId"] != "s1abc1234567" {
			t.Errorf("Expected stagiaireId s1abc1234567, got %v", resp["stagiaireId"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for stagiaire response")
	}

	// Trainer should receive connected_count when stagiaire joins
	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "connected_count" {
			t.Errorf("Expected connected_count, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for trainer connected_count")
	}

	time.Sleep(50 * time.Millisecond)

	// 3. Test Start Vote (Trainer)
	startVoteMsg := models.Message{
		Type:           "start_vote",
		Colors:         []string{"rouge", "bleu"},
		MultipleChoice: false,
	}
	startVoteBytes, _ := json.Marshal(startVoteMsg)
	trainer.handleMessage(startVoteBytes)

	// Verify broadcast happened (stagiaire should receive vote_started)
	// Since stagiaire is registered, it should get a message via its Send channel
	select {
	case msg := <-stagiaire.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "vote_started" {
			t.Errorf("Expected vote_started for stagiaire, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for vote_started broadcast")
	}

    // Trainer also receives vote_started since excludeID is empty
    select {
    case msg := <-trainer.Send:
        var resp map[string]interface{}
        json.Unmarshal(msg, &resp)
        if resp["type"] != "vote_started" {
             t.Errorf("Expected vote_started for trainer, got %v", resp["type"])
        }
    case <-time.After(time.Second):
        t.Error("Timeout waiting for vote_started for trainer")
    }

	// 4. Test Submit Vote (Stagiaire)
	voteMsg := models.Message{
		Type:   "vote",
		Colors: []string{"rouge"},
	}
	voteBytes, _ := json.Marshal(voteMsg)
	stagiaire.handleMessage(voteBytes)

	// Stagiaire gets ack
	select {
	case msg := <-stagiaire.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "vote_accepted" {
			t.Errorf("Expected vote_accepted, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for vote ack")
	}

	// Trainer gets connected_count first (from notifyTrainerStagiaireList)
	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "connected_count" {
			t.Errorf("Expected connected_count, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for trainer connected_count")
	}

	// Then trainer gets vote_received notification
	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "vote_received" {
			t.Errorf("Expected vote_received, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for trainer vote notification")
	}

	// 5. Test Update Name
	updateNameMsg := models.Message{
		Type: "update_name",
		Name: "Robert",
	}
	updateBytes, _ := json.Marshal(updateNameMsg)
	stagiaire.handleMessage(updateBytes)

	// Stagiaire gets ack
	select {
	case msg := <-stagiaire.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "name_updated" {
			t.Errorf("Expected name_updated, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for name update ack")
	}

    if stagiaire.Name != "Robert" {
        t.Errorf("Stagiaire name not updated in struct, got %s", stagiaire.Name)
    }

	// 6. Test Reset Vote
	resetMsg := models.Message{
		Type: "reset_vote",
	}
	resetBytes, _ := json.Marshal(resetMsg)
	trainer.handleMessage(resetBytes)

	select {
	case msg := <-stagiaire.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "vote_reset" {
			t.Errorf("Expected vote_reset, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for vote_reset")
	}

	// 7. Test Close Vote
	closeMsg := models.Message{
		Type: "close_vote",
	}
	closeMsgBytes, _ := json.Marshal(closeMsg)
	trainer.handleMessage(closeMsgBytes)

	select {
	case msg := <-stagiaire.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "vote_closed" {
			t.Errorf("Expected vote_closed, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for vote_closed")
	}
}

func TestClientHandleErrors(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors: []string{
			"rouge", "vert", "bleu", "jaune",
			"orange", "violet", "rose", "gris",
		},
	}
	h := NewHub(cfg)
	go h.Run()
	defer h.Shutdown()

	client := &Client{
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}
	initTestHandlers(client)

	// Test malformed JSON
	client.handleMessage([]byte("{invalid-json"))
	// Should log error but not crash (we can't easily check log output here, but we ensure no panic)

	// Test unknown message type
	unknownMsg := models.Message{Type: "unknown_type"}
	unknownBytes, _ := json.Marshal(unknownMsg)
	client.handleMessage(unknownBytes)
	// Should warn but not crash

	// Test Invalid Session Code (Trainer Join)
	// Empty session code now triggers server generation, so we test an invalid format
	invalidJoinMsg := models.Message{
		Type:        "trainer_join",
		SessionCode: "abcd", // Not 4 digits - invalid format
	}
	invalidJoinBytes, _ := json.Marshal(invalidJoinMsg)
	client.handleMessage(invalidJoinBytes)

	select {
	case msg := <-client.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "error" {
			t.Errorf("Expected error for invalid session, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for error response")
	}

	// Test Invalid Session Code (Stagiaire Join) - covers SendErrorWithBackoff
	invalidStagiaireMsg := models.Message{
		Type:        "stagiaire_join",
		SessionCode: "abc", // Not 4 digits - invalid format
	}
	invalidStagiaireBytes, _ := json.Marshal(invalidStagiaireMsg)
	client.handleMessage(invalidStagiaireBytes)

	select {
	case msg := <-client.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "error" {
			t.Errorf("Expected error for invalid stagiaire session, got %v", resp["type"])
		}
		// No backoffMs should be present (security fix - no timing disclosure)
		if _, ok := resp["backoffMs"]; ok {
			t.Error("backoffMs should not be present in error response (security fix)")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for stagiaire error response")
	}
}
