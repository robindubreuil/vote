package hub

import (
	"encoding/json"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/models"
)

func TestClientHandleMessage(t *testing.T) {
	// Setup Hub
	cfg := &config.Config{
        SessionTimeout: time.Hour,
        CleanupInterval: time.Hour,
        PingInterval: time.Second, // Need positive ping interval for NewClient
    }
	h := NewHub(cfg)
	go h.Run()
	defer h.Shutdown()

	// 1. Test Trainer Join
	trainer := &Client{
		ID:   "trainer1",
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}

	joinMsg := models.Message{
		Type:        "trainer_join",
		SessionCode: "1234",
	}
	joinBytes, _ := json.Marshal(joinMsg)
	trainer.handleMessage(joinBytes)

	// Verify trainer received session_created
	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "session_created" {
			t.Errorf("Expected session_created, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for trainer response")
	}

	// Wait for hub to process registration
	time.Sleep(50 * time.Millisecond)

	if !h.SessionExists("1234") {
		t.Error("Session 1234 should exist")
	}

	// 2. Test Stagiaire Join
	stagiaire := &Client{
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}
	stJoinMsg := models.Message{
		Type:        "stagiaire_join",
		SessionCode: "1234",
		StagiaireID: "s1",
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
		Colors:         []string{"red", "blue"},
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
		Colors: []string{"red"},
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

	// Trainer gets notification
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
	closeBytes, _ := json.Marshal(closeMsg)
	trainer.handleMessage(closeBytes)

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
        SessionTimeout: time.Hour,
        CleanupInterval: time.Hour,
        PingInterval: time.Second,
    }
	h := NewHub(cfg)
	go h.Run()
	defer h.Shutdown()

	client := &Client{
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}

	// Test malformed JSON
	client.handleMessage([]byte("{invalid-json"))
	// Should log error but not crash (we can't easily check log output here, but we ensure no panic)

	// Test unknown message type
	unknownMsg := models.Message{Type: "unknown_type"}
	unknownBytes, _ := json.Marshal(unknownMsg)
	client.handleMessage(unknownBytes)
	// Should warn but not crash

	// Test Invalid Session Code (Trainer Join)
	invalidJoinMsg := models.Message{
		Type:        "trainer_join",
		SessionCode: "", // Empty is invalid
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
		SessionCode: "invalid",
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
		if _, ok := resp["backoffMs"]; !ok {
			t.Error("Expected backoffMs in error response")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for stagiaire error response")
	}
}
