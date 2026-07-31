package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/config"
	"vote-backend/internal/models"
	"vote-backend/internal/security"
)

func initTestHandlers(c *Client) {
	c.handlers = map[string]func(models.Message){
		"trainer_join":      c.handleTrainerJoin,
		"stagiaire_join":    c.handleStagiaireJoin,
		"start_vote":        c.handleStartVote,
		"vote":              c.handleVote,
		"close_vote":        c.handleCloseVote,
		"reset_vote":        c.handleResetVote,
		"reveal_answers":    c.handleRevealAnswers,
		"report_game_score": c.handleReportGameScore,
		"update_name":       c.handleUpdateName,
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
	h.Run()
	defer h.Shutdown()

	// 1. Test Trainer Join - use "new" to create a session
	trainer := &Client{
		ID:   "trainer1abcde",
		Hub:  h,
		Send: make(chan []byte, 10),
		IP:   "127.0.0.1",
	}
	initTestHandlers(trainer)

	joinMsg := models.Message{
		Type:        "trainer_join",
		SessionCode: "new",
	}
	joinBytes, _ := json.Marshal(joinMsg)
	trainer.handleMessage(joinBytes)

	// Trainer receives 2 messages upon joining a fresh session:
	// 1. connected_count (from registerClient)
	// 2. session_created (from handleTrainerJoin)
	// config_updated is NOT sent on a fresh session — the backend only syncs
	// config when the session has been configured (non-empty colors), to
	// avoid clobbering the client's autoloaded last-config.
	// We need to consume all of them to clear the channel for subsequent tests.

	var sessionCode string
	expectedTypes := map[string]bool{
		"connected_count": true,
		"session_created": true,
	}

	for i := 0; i < 2; i++ {
		select {
		case msg := <-trainer.Send:
			var resp map[string]interface{}
			json.Unmarshal(msg, &resp)
			msgType := resp["type"].(string)
			if !expectedTypes[msgType] {
				t.Errorf("Unexpected message type during join: %v", msgType)
			}
			// Capture the generated session code
			if msgType == "session_created" {
				if code, ok := resp["sessionCode"].(string); ok {
					sessionCode = code
				}
			}
			delete(expectedTypes, msgType)
		case <-time.After(time.Second):
			t.Error("Timeout waiting for trainer join messages")
		}
	}

	if len(expectedTypes) > 0 {
		t.Errorf("Did not receive all expected messages. Missing: %v", expectedTypes)
	}

	if sessionCode == "" {
		t.Fatal("Expected session code to be generated")
	}

	// Wait for hub to process registration
	time.Sleep(50 * time.Millisecond)

	if !h.SessionExists(sessionCode) {
		t.Errorf("Session %s should exist", sessionCode)
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
		SessionCode: sessionCode,
		// StagiaireID is no longer used - ID comes from client.ID (server-generated)
		Name: "Bob",
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
	h.Run()
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

func TestCompetitiveRevealFlow(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	time.Sleep(50 * time.Millisecond)

	stagiaire := &Client{ID: "s1abc1234567", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(stagiaire)
	stagiaire.handleMessage(mustMarshal(t, models.Message{
		Type: "stagiaire_join", SessionCode: sessionCode, Name: "Alice",
	}))
	drainUntil(t, stagiaire, "session_joined")
	drainN(t, trainer, 1)

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "start_vote", Colors: []string{"rouge", "bleu"}, Competitive: true,
	}))
	drainN(t, trainer, 3)
	drainUntil(t, stagiaire, "vote_started")

	stagiaire.handleMessage(mustMarshal(t, models.Message{Type: "vote", Colors: []string{"rouge"}}))
	drainUntil(t, stagiaire, "vote_accepted")
	drainN(t, trainer, 2)

	trainer.handleMessage(mustMarshal(t, models.Message{Type: "close_vote"}))
	drainN(t, trainer, 1)
	drainUntil(t, stagiaire, "vote_closed")

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "reveal_answers", CorrectColors: []string{"rouge"},
	}))

	trainerMsg := drainUntil(t, trainer, "answers_revealed")
	scores := trainerMsg["scores"].([]interface{})
	if len(scores) != 1 {
		t.Fatalf("expected 1 score entry, got %d", len(scores))
	}
	entry := scores[0].(map[string]interface{})
	if int(entry["voteScore"].(float64)) != 2000 {
		t.Errorf("expected voteScore 2000, got %v", entry["voteScore"])
	}

	stagiaireMsg := drainUntil(t, stagiaire, "answers_revealed")
	if int(stagiaireMsg["voteScore"].(float64)) != 2000 {
		t.Errorf("stagiaire voteScore: expected 2000, got %v", stagiaireMsg["voteScore"])
	}
	if stagiaireMsg["gameScore"] != nil && int(stagiaireMsg["gameScore"].(float64)) != 0 {
		t.Errorf("expected gameScore 0, got %v", stagiaireMsg["gameScore"])
	}
}

func TestTrainerGuards(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	time.Sleep(50 * time.Millisecond)

	imposter := &Client{ID: "s2abc1234567", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(imposter)
	imposter.Type = "stagiaire"
	imposter.SessionID = sessionCode

	imposter.handleMessage(mustMarshal(t, models.Message{Type: "start_vote", Colors: []string{"rouge"}}))
	drainOrTimeout(t, imposter)

	imposter.handleMessage(mustMarshal(t, models.Message{Type: "close_vote"}))
	drainOrTimeout(t, imposter)

	imposter.handleMessage(mustMarshal(t, models.Message{Type: "reset_vote"}))
	drainOrTimeout(t, imposter)

	imposter.handleMessage(mustMarshal(t, models.Message{Type: "reveal_answers", CorrectColors: []string{"rouge"}}))
	drainOrTimeout(t, imposter)
}

func TestGameScoreValidation(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	time.Sleep(50 * time.Millisecond)

	stagiaire := &Client{ID: "s1abc1234567", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(stagiaire)
	stagiaire.handleMessage(mustMarshal(t, models.Message{
		Type: "stagiaire_join", SessionCode: sessionCode, Name: "Alice",
	}))
	drainUntil(t, stagiaire, "session_joined")
	drainN(t, trainer, 1)

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "start_vote", Colors: []string{"rouge"}, GameEnabled: true, Competitive: true,
	}))
	drainN(t, trainer, 3)
	drainUntil(t, stagiaire, "vote_started")

	stagiaire.handleMessage(mustMarshal(t, models.Message{Type: "report_game_score", GameScore: -1}))
	drainOrTimeout(t, stagiaire)

	stagiaire.handleMessage(mustMarshal(t, models.Message{Type: "report_game_score", GameScore: MaxGameScore + 1}))
	drainOrTimeout(t, stagiaire)

	stagiaire.handleMessage(mustMarshal(t, models.Message{Type: "report_game_score", GameScore: 500}))
	drainUntil(t, trainer, "connected_count")

	session, _ := h.VoteManager.GetSession(sessionCode)
	if session.GetGameScores()["s1abc1234567"] != 500 {
		t.Errorf("expected game score 500, got %d", session.GetGameScores()["s1abc1234567"])
	}
}

func TestRevealRejectsColorsOutsidePalette(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	drainUntil(t, trainer, "session_created")
	time.Sleep(50 * time.Millisecond)

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "start_vote", Colors: []string{"rouge", "bleu"}, Competitive: true,
	}))
	drainN(t, trainer, 3)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "close_vote"}))
	drainN(t, trainer, 1)

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "reveal_answers", CorrectColors: []string{"vert"},
	}))

	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "error" {
			t.Errorf("expected error for vert (not in palette), got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for error")
	}
}

func TestHandleVoteRejectsTrainer(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	time.Sleep(50 * time.Millisecond)

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "start_vote", Colors: []string{"rouge"},
	}))
	drainN(t, trainer, 3)

	// The trainer (c.Type == "trainer" by default after trainer_join) attempts
	// to vote. This must be rejected, not recorded.
	trainer.SessionID = sessionCode
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:   "vote",
		Colors: []string{"rouge"},
	}))

	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "error" {
			t.Errorf("trainer vote should be rejected with error, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for rejection")
	}

	session, ok := h.VoteManager.GetSession(sessionCode)
	if !ok {
		t.Fatal("session should exist")
	}
	if votes := session.GetVotes(); len(votes) != 0 {
		t.Errorf("trainer vote should not be recorded, got %d votes", len(votes))
	}
}

func TestHandleResetVotePreservesLabels(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	time.Sleep(50 * time.Millisecond)

	labels := map[string]string{"rouge": "Pomme"}
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:           "start_vote",
		Colors:         []string{"rouge"},
		Labels:         labels,
		MultipleChoice: false,
	}))
	drainN(t, trainer, 3)

	// reset_vote with the same labels — previously the handler hard-coded nil
	// and wiped the labels from the session state.
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:   "reset_vote",
		Colors: []string{"rouge"},
		Labels: labels,
	}))
	drainUntil(t, trainer, "vote_reset")

	session, ok := h.VoteManager.GetSession(sessionCode)
	if !ok {
		t.Fatal("session should exist")
	}
	got := session.GetActiveLabels()
	if got["rouge"] != "Pomme" {
		t.Errorf("label should survive reset_vote, got %v", got)
	}
}

// TestStartVoteRejectsLabelsOutsidePalette covers BM5: labels must
// reference colors in the selected palette (msg.Colors), not the
// global ValidColors. A trainer must not be able to attach a label to
// a color that isn't on the ballot.
func TestStartVoteRejectsLabelsOutsidePalette(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	drainUntil(t, trainer, "session_created")
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Labels include "vert", which is a globally-valid color but is NOT
	// in the selected palette {rouge, bleu}. Must be rejected.
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:   "start_vote",
		Colors: []string{"rouge", "bleu"},
		Labels: map[string]string{"rouge": "Pomme", "vert": "Salade"},
	}))

	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "error" {
			t.Errorf("expected error for label outside palette, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for label-validation error")
	}

	// Sanity: the same label set WITH vert in the palette is accepted.
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:   "start_vote",
		Colors: []string{"rouge", "bleu", "vert"},
		Labels: map[string]string{"rouge": "Pomme", "vert": "Salade"},
	}))
	drainUntil(t, trainer, "vote_started")
}

// TestResetVoteRejectsLabelsOutsidePalette covers BM5 on the reset path.
func TestResetVoteRejectsLabelsOutsidePalette(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	drainUntil(t, trainer, "session_created")
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	trainer.handleMessage(mustMarshal(t, models.Message{
		Type:   "reset_vote",
		Colors: []string{"rouge"},
		Labels: map[string]string{"vert": "Salade"},
	}))
	select {
	case msg := <-trainer.Send:
		var resp map[string]interface{}
		json.Unmarshal(msg, &resp)
		if resp["type"] != "error" {
			t.Errorf("expected error for label outside palette on reset, got %v", resp["type"])
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for label-validation error")
	}
}

// TestTrainerTakeoverRequiresToken covers S1: an unauthenticated
// trainer_join against a session with an active trainer must be rejected.
// Only a connection presenting the minted token can take over.
func TestTrainerTakeoverRequiresToken(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// 1. Legitimate trainer creates the session.
	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCreated := drainUntil(t, trainer, "session_created")
	sessionCode := sessionCreated["sessionCode"].(string)
	token, _ := sessionCreated["trainerToken"].(string)
	if token == "" {
		t.Fatal("session_created must carry a trainerToken")
	}
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// 2. Imposter tries to take over WITHOUT the token → must be rejected.
	imposter := &Client{ID: "imposter00001", Hub: h, Send: make(chan []byte, 20), IP: "10.0.0.99"}
	initTestHandlers(imposter)
	imposter.handleMessage(mustMarshal(t, models.Message{
		Type:        "trainer_join",
		SessionCode: sessionCode,
	}))
	imposterMsg := drainUntil(t, imposter, "error")
	if msg, _ := imposterMsg["message"].(string); msg == "" {
		t.Error("imposter should receive a rejection error")
	}

	// The legitimate trainer must still be the registered trainer.
	h.mu.RLock()
	active := h.Connections[sessionCode].Trainer
	h.mu.RUnlock()
	if active != trainer {
		t.Error("imposter must not have displaced the legitimate trainer")
	}

	// 3. A reconnect with the correct token takes over successfully.
	reconnect := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(reconnect)
	reconnect.handleMessage(mustMarshal(t, models.Message{
		Type:         "trainer_join",
		SessionCode:  sessionCode,
		TrainerToken: token,
	}))
	drainUntil(t, reconnect, "session_created")
	drainUntil(t, reconnect, "connected_count")

	h.mu.RLock()
	active = h.Connections[sessionCode].Trainer
	h.mu.RUnlock()
	if active != reconnect {
		t.Error("reconnect with valid token should become the active trainer")
	}
}

// TestEmptyTrainerSlotRequiresToken covers S13: once the legitimate
// trainer's readPump exits and unregisterClient clears conns.Trainer,
// the empty slot must NOT be claimable with only the public 3-char
// session code. The previous code skipped the trainer-token check on
// the recovery path AND re-emitted the minted token in session_created,
// so an attacker who knew the code (printed in every stagiaire's QR)
// could take the slot, receive the token, and hold the session for the
// rest of its life. The fix requires the per-session token on every
// join to an already-minted Manager session; the legitimate trainer
// always has it (sessionStorage, scoped to the tab), and an
// unauthenticated claimant is rejected with the same "Session
// introuvable" message the no-session branch uses (so the response
// does not leak that the code is live).
func TestEmptyTrainerSlotRequiresToken(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// 1. Legitimate trainer creates the session and receives the token.
	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCreated := drainUntil(t, trainer, "session_created")
	sessionCode := sessionCreated["sessionCode"].(string)
	token, _ := sessionCreated["trainerToken"].(string)
	if token == "" {
		t.Fatal("session_created must carry a trainerToken")
	}
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// 2. Drain the trainer via the real path: simulate readPump exit by
	// routing through Unregister so unregisterClient sets
	// conns.Trainer = nil — the S13 precondition. The Manager session
	// (and its minted token) survives.
	h.Unregister <- trainer
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	active := h.Connections[sessionCode].Trainer
	h.mu.RUnlock()
	if active != nil {
		t.Fatalf("precondition: trainer slot must be empty after unregister, got %v", active)
	}
	if _, ok := h.VoteManager.GetSession(sessionCode); !ok {
		t.Fatal("precondition: Manager session must survive trainer disconnect")
	}

	// 3. Imposter (knows only the public code) tries to claim the empty
	// slot WITHOUT the token → must be rejected with "Session
	// introuvable" (not session_created, and not the active-takeover
	// wording — collapsing the oracle).
	imposter := &Client{ID: "imposter00001", Hub: h, Send: make(chan []byte, 20), IP: "10.0.0.99"}
	initTestHandlers(imposter)
	imposter.handleMessage(mustMarshal(t, models.Message{
		Type:        "trainer_join",
		SessionCode: sessionCode,
	}))

	rej := drainUntil(t, imposter, "error")
	if msg, _ := rej["message"].(string); msg != "Session introuvable" {
		t.Errorf("imposter rejection: got %q, want %q (S13: oracle-collapse)", msg, "Session introuvable")
	}
	// Drain anything else the imposter might have queued (defense).
	drainOrTimeout(t, imposter)

	h.mu.RLock()
	active = h.Connections[sessionCode].Trainer
	h.mu.RUnlock()
	if active != nil {
		t.Errorf("imposter must not claim the empty slot, got trainer=%v", active)
	}

	// 4. The legitimate trainer (carrying the persisted token) recovers
	// the empty slot — the documented "trainer can always take it back"
	// path. S13 keeps this working because the legit trainer has the
	// token.
	recovery := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(recovery)
	recovery.handleMessage(mustMarshal(t, models.Message{
		Type:         "trainer_join",
		SessionCode:  sessionCode,
		TrainerToken: token,
	}))
	drainUntil(t, recovery, "session_created")
	drainUntil(t, recovery, "connected_count")

	h.mu.RLock()
	active = h.Connections[sessionCode].Trainer
	h.mu.RUnlock()
	if active != recovery {
		t.Errorf("legitimate recovery with token must claim the slot, got %v", active)
	}
}

// TestTrainerTokenNeverReEmittedToTokenlessClaim covers the second half
// of S13: the recovery path must NOT hand the minted token to an
// unauthenticated claimant. The previous code unconditionally ran the
// "emit token in session_created" block once conns.Trainer was set, so
// an attacker who claimed the empty slot received the same secret the
// legitimate trainer held — defeating the token gate for every
// subsequent reconnect. With the fix the unauthenticated claim is
// rejected before the emit block runs.
func TestTrainerTokenNeverReEmittedToTokenlessClaim(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCreated := drainUntil(t, trainer, "session_created")
	sessionCode := sessionCreated["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Drop the active trainer to open the empty-slot path.
	h.Unregister <- trainer
	time.Sleep(50 * time.Millisecond)

	// Claimant with no token attempts recovery.
	claimant := &Client{ID: "claimant00001", Hub: h, Send: make(chan []byte, 20), IP: "10.0.0.7"}
	initTestHandlers(claimant)
	claimant.handleMessage(mustMarshal(t, models.Message{
		Type:        "trainer_join",
		SessionCode: sessionCode,
	}))

	// Drain every message the claimant received. None of them may be
	// session_created (which would carry the token), and none may
	// include a trainerToken field.
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-claimant.Send:
			var resp map[string]interface{}
			if err := json.Unmarshal(msg, &resp); err != nil {
				t.Fatalf("unmarshal claimant message: %v", err)
			}
			if resp["type"] == "session_created" {
				t.Errorf("claimant received session_created: %v — S13 forbids token emission to unauthenticated claimants", resp)
			}
			if _, ok := resp["trainerToken"]; ok {
				t.Errorf("claimant received trainerToken field: %v — S13 forbids token leakage", resp)
			}
		case <-deadline:
			return
		}
	}
}

func mustMarshal(t *testing.T, msg models.Message) []byte {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func drainUntil(t *testing.T, c *Client, wantType string) map[string]interface{} {
	t.Helper()
	for i := 0; i < 20; i++ {
		select {
		case msg := <-c.Send:
			var resp map[string]interface{}
			json.Unmarshal(msg, &resp)
			if resp["type"] == wantType {
				return resp
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %s", wantType)
		}
	}
	t.Fatalf("drained 20 messages without finding %s", wantType)
	return nil
}

func drainN(t *testing.T, c *Client, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-c.Send:
		case <-time.After(time.Second):
			t.Fatalf("timeout draining message %d/%d", i+1, n)
		}
	}
}

func drainOrTimeout(t *testing.T, c *Client) {
	t.Helper()
	select {
	case <-c.Send:
	case <-time.After(100 * time.Millisecond):
	}
}

// TestTrainerReconnectReceivesFullConfig is the FH4 regression test. When
// a trainer reconnects to a session that is Idle but has been configured
// (colors set, plus labels / feature flags), the server replays the full
// config surface via config_updated. Before the fix it sent only
// selectedColors + multipleChoice, so a trainer reconnecting on a
// different device (or after another trainer reconfigured) lost every
// other field and could silently re-start the vote with stale settings.
func TestTrainerReconnectReceivesFullConfig(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer1 := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer1)
	trainer1.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer1, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer1, "connected_count")

	// Configure via reset_vote — lands the session in state=Idle with
	// ActiveColors populated, which is exactly the FH4 reconnect path.
	labels := map[string]string{"rouge": "Pomme", "bleu": "Ciel"}
	trainer1.handleMessage(mustMarshal(t, models.Message{
		Type:           "reset_vote",
		Colors:         []string{"rouge", "bleu"},
		Labels:         labels,
		MultipleChoice: true,
		GameEnabled:    true,
		Competitive:    true,
		AllowBlank:     true,
	}))
	// reset_vote broadcasts vote_reset to every client (including the
	// trainer that issued it), plus connected_count — drain them.
	drainUntil(t, trainer1, "vote_reset")
	drainUntil(t, trainer1, "connected_count")

	// A second trainer joins with the token — simulates a reconnect
	// from a different device. The server's Idle+colors branch fires.
	trainer2 := &Client{ID: "trainer2fghijk", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer2)
	// Pre-load the token so the takeover gate (S1) accepts trainer2.
	if sess, ok := h.VoteManager.GetSession(sessionCode); !ok || sess.GetTrainerToken() == "" {
		t.Fatal("missing trainer token on session")
	} else {
		trainer2.TrainerToken = sess.GetTrainerToken()
	}
	trainer2.handleMessage(mustMarshal(t, models.Message{
		Type:         "trainer_join",
		SessionCode:  sessionCode,
		TrainerToken: trainer2.TrainerToken,
	}))

	// Trainer2 should see: session_created, connected_count, then the
	// config_updated replay. Drain until we find config_updated.
	drainUntil(t, trainer2, "session_created")
	drainUntil(t, trainer2, "connected_count")
	cfgMsg := drainUntil(t, trainer2, "config_updated")

	if got := cfgMsg["selectedColors"]; !sameStrings(got, []string{"rouge", "bleu"}) {
		t.Errorf("selectedColors: got %v, want [rouge bleu]", got)
	}
	if mc, _ := cfgMsg["multipleChoice"].(bool); !mc {
		t.Errorf("multipleChoice not replayed: %v", cfgMsg["multipleChoice"])
	}
	if ge, _ := cfgMsg["gameEnabled"].(bool); !ge {
		t.Errorf("gameEnabled not replayed: %v", cfgMsg["gameEnabled"])
	}
	if comp, _ := cfgMsg["competitive"].(bool); !comp {
		t.Errorf("competitive not replayed: %v", cfgMsg["competitive"])
	}
	if ab, _ := cfgMsg["allowBlank"].(bool); !ab {
		t.Errorf("allowBlank not replayed: %v", cfgMsg["allowBlank"])
	}
	if lab, ok := cfgMsg["labels"].(map[string]any); !ok {
		t.Errorf("labels not replayed: %v", cfgMsg["labels"])
	} else if lab["rouge"] != "Pomme" || lab["bleu"] != "Ciel" {
		t.Errorf("labels mismatch: %v", lab)
	}
}

// sameStrings compares an `[]any` from JSON to a literal []string.
func sameStrings(got any, want []string) bool {
	arr, ok := got.([]any)
	if !ok || len(arr) != len(want) {
		return false
	}
	for i, w := range want {
		if s, _ := arr[i].(string); s != w {
			return false
		}
	}
	return true
}

// TestJoinStagiaireRetryResetsClientID covers R1: when a stagiaire_join
// presents a known ID + missing/wrong reclaim token, the server rejects
// it. The frontend then retries with an empty stagiaireId (dropping the
// stale credentials). Before R1, c.ID stayed at the rejected value
// because the ID-resolution guard is skipped on empty StagiaireID, so
// JoinStagiaire re-took the reclaim path with the stale ID + empty
// token and failed again — a tight message loop throttled only by the
// per-client rate cap. With R1, handleStagiaireJoin resets c.ID to the
// immutable OriginalID at the top of every attempt, so the retry is
// treated as a fresh join and succeeds.
func TestJoinStagiaireRetryResetsClientID(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	code := drainUntil(t, trainer, "session_created")["sessionCode"].(string)
	drainUntil(t, trainer, "connected_count")
	time.Sleep(50 * time.Millisecond)

	// Bootstrap a real stagiaire identity directly in the Manager so a
	// presented stale ID is "known" to the session — the precondition
	// for JoinStagiaire's reclaim path. The ID below is what the
	// frontend would have cached in sessionStorage from a prior join
	// (e.g. before a tab crash left the reclaim token unset). Use a
	// distinct name so the R1 retry's fresh-join name-collision check
	// (S6 — names are reserved until the trainer resets) doesn't
	// obscure the c.ID reset assertion we actually care about.
	staleID := "stale1234567"
	if _, err := h.VoteManager.JoinStagiaire(code, staleID, "PriorUser", ""); err != nil {
		t.Fatalf("bootstrap JoinStagiaire: %v", err)
	}

	// WS handshake mints an immutable OriginalID. The first join
	// presents the cached stale ID with NO reclaim token (the partial-
	// sessionStorage-failure scenario from R1) and is rejected via the
	// reclaim path.
	c := &Client{ID: staleID, OriginalID: "origabc12345", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(c)
	c.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		StagiaireID: staleID,
		Name:        "Marie",
	}))
	rej := drainUntil(t, c, "error")
	if msg, _ := rej["message"].(string); msg == "" {
		t.Fatal("first join with stale ID + no token should be rejected")
	}

	// Sanity: the advisory path overwrote c.ID with the presented ID.
	if c.ID != staleID {
		t.Fatalf("first join should leave c.ID == presented stale ID, got %q", c.ID)
	}

	// Frontend retry: empty stagiaireId. Before R1 the server-side c.ID
	// was still stale, so the retry re-entered JoinStagiaire with the
	// rejected ID + empty token and failed again. With R1, c.ID is reset
	// to OriginalID at the top of handleStagiaireJoin, so the retry is a
	// fresh join that mints a new identity.
	c.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: code,
		Name:        "Marie",
	}))

	joined := drainUntil(t, c, "session_joined")
	if got, _ := joined["stagiaireId"].(string); got != "origabc12345" {
		t.Errorf("retry should join as the OriginalID, got stagiaireId=%q want origabc12345", got)
	}

	// Defensive: c.ID must be OriginalID after the retry (proving the
	// reset happened — without R1 this would still be the stale ID).
	if c.ID != c.OriginalID {
		t.Errorf("post-retry c.ID = %q, want OriginalID %q (R1 reset missing)", c.ID, c.OriginalID)
	}

	// The fresh OriginalID must be registered. The staleID is also
	// still there (S6: only a trainer reset clears it).
	sess, ok := h.VoteManager.GetSession(code)
	if !ok {
		t.Fatal("session missing after retry")
	}
	stagiaires := sess.GetStagiaires()
	if _, ok := stagiaires["origabc12345"]; !ok {
		t.Errorf("OriginalID not registered: %v", stagiaires)
	}
}

// TestJoinHandlerEnforcesBackoff covers S14: the per-IP exponential
// backoff (S2) must apply to join attempts sent on an ESTABLISHED
// WebSocket, not only to the WS upgrade handshake. Before S14 the only
// in-connection bound was the per-client message rate (10/s burst 20),
// so a single upgraded WebSocket could probe trainer_join /
// stagiaire_join against arbitrary codes at that rate — the ~12,167-code
// space enumerable in ~20 min from one connection, with the
// "Session introuvable" oracle handing live codes to S13. The fix calls
// Security.CheckJoinRateLimit(c.IP) at the top of both join handlers.
func TestJoinHandlerEnforcesBackoff(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	// Prime the IP into backoff by racking up MaxFailedAttempts failed
	// joins via the stagiaire handler. SendErrorWithBackoff accrues
	// failures through RecordFailedJoin; after MaxFailedAttempts the
	// next attempt is in the exponential backoff window.
	const attackerIP = "203.0.113.7"
	c := &Client{ID: "atk000000001", OriginalID: "atk000000001", Hub: h, Send: make(chan []byte, 30), IP: attackerIP}
	initTestHandlers(c)

	// Send MaxFailedAttempts bad-code stagiaire_joins. Each records a
	// failure and replies with an error; drain them so the send buffer
	// doesn't backpressure the loop. "PPP" is a syntactically-valid
	// code (session alphabet excludes only I/O/Z) that won't exist on a
	// fresh hub.
	for i := 0; i < security.MaxFailedAttempts; i++ {
		c.handleMessage(mustMarshal(t, models.Message{
			Type:        "stagiaire_join",
			SessionCode: "PPP", // syntactically valid, non-existent
			Name:        "Attacker",
		}))
		if resp := drainUntil(t, c, "error"); resp["message"] != "Session introuvable" {
			t.Fatalf("priming join %d: got %v, want 'Session introuvable'", i, resp["message"])
		}
	}

	// Sanity: the IP is now in backoff. The next CheckJoinRateLimit
	// call must deny it (not just record-then-allow).
	if h.Security.CheckJoinRateLimit(attackerIP) {
		t.Fatal("precondition: IP must be in backoff after MaxFailedAttempts failures")
	}

	// 1. stagiaire_join on the established connection is rejected by
	// the per-handler S14 gate — the message never reaches the
	// advisory "Session introuvable" path (which would emit a
	// different error string).
	c.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: "PPP",
		Name:        "Attacker",
	}))
	resp := drainUntil(t, c, "error")
	if msg, _ := resp["message"].(string); msg != "Trop de tentatives — réessayez dans quelques minutes" {
		t.Errorf("stagiaire_join under backoff: got %q, want backoff message", msg)
	}

	// 2. trainer_join on the same connection is also rejected by the
	// per-handler S14 gate. Use a valid code shape so the rejection
	// must come from the backoff check, not from the code validator.
	c.handleMessage(mustMarshal(t, models.Message{
		Type:        "trainer_join",
		SessionCode: "PPP",
	}))
	resp = drainUntil(t, c, "error")
	if msg, _ := resp["message"].(string); msg != "Trop de tentatives — réessayez dans quelques minutes" {
		t.Errorf("trainer_join under backoff: got %q, want backoff message", msg)
	}

	// 3. A different IP is unaffected — backoff is per-IP, not global.
	// This is the property that lets a shared-NAT classroom absorb one
	// attacker's backoff without locking out everyone (the threat
	// model from S2/S16).
	other := &Client{ID: "other0000001", OriginalID: "other0000001", Hub: h, Send: make(chan []byte, 30), IP: "198.51.100.42"}
	initTestHandlers(other)
	other.handleMessage(mustMarshal(t, models.Message{
		Type:        "stagiaire_join",
		SessionCode: "PPP",
		Name:        "Other",
	}))
	otherResp := drainUntil(t, other, "error")
	if msg, _ := otherResp["message"].(string); msg == "Trop de tentatives — réessayez dans quelques minutes" {
		t.Errorf("other IP wrongly throttled: backoff must be per-IP, not global (got %q)", msg)
	}
}

// TestHandleUpdateNameRejectsTrainer covers B1: handleUpdateName was the
// only mutation handler without a role gate. Today the fallthrough is
// harmless (UpdateStagiaireName returns ErrStagiaireNotFound for a trainer
// ID), but it is a single-point inconsistency a future refactor could turn
// into a real privilege issue. The gate mirrors every other mutation
// handler and makes the rejection explicit.
func TestHandleUpdateNameRejectsTrainer(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	trainer := &Client{ID: "trainer1abcde", Hub: h, Send: make(chan []byte, 20), IP: "127.0.0.1"}
	initTestHandlers(trainer)
	trainer.handleMessage(mustMarshal(t, models.Message{Type: "trainer_join", SessionCode: "new"}))
	sessionCode := drainUntil(t, trainer, "session_created")["sessionCode"].(string)

	// trainer_join set c.Type = "trainer". A trainer (or pre-join
	// anonymous client) must not mutate stagiaire state via update_name.
	trainer.SessionID = sessionCode
	trainer.handleMessage(mustMarshal(t, models.Message{
		Type: "update_name",
		Name: "Whatever",
	}))

	resp := drainUntil(t, trainer, "error")
	if msg, _ := resp["message"].(string); msg == "" {
		t.Fatalf("trainer update_name should be rejected with an error, got %v", resp)
	}

	// No stagiaire identity may have been created or mutated: the role
	// gate must run before UpdateStagiaireName is even called.
	session, ok := h.VoteManager.GetSession(sessionCode)
	if !ok {
		t.Fatal("session should exist")
	}
	if _, exists := session.GetStagiaires()[trainer.ID]; exists {
		t.Errorf("trainer ID must not appear in Stagiaires after rejected update_name")
	}
}

// TestRateLimitKeyStableAcrossIDCycles covers R22: readPump's defer calls
// RemoveMessageRate(c.ID), but c.ID is mutable across stagiaire_join
// messages (R1 resets it to OriginalID, then a presented ID overwrites
// it). Keying the rate limiter by the mutable c.ID leaked earlier buckets
// on disconnect (only the final c.ID's entry was removed) AND refreshed
// the MaxBurstMessages budget per ID cycle. rateLimitKey() returns the
// immutable OriginalID so one connection owns exactly one bucket.
func TestRateLimitKeyStableAcrossIDCycles(t *testing.T) {
	t.Run("rateLimitKey returns immutable OriginalID", func(t *testing.T) {
		c := &Client{ID: "presented1234", OriginalID: "origabc12345"}
		if got := c.rateLimitKey(); got != "origabc12345" {
			t.Errorf("rateLimitKey = %q, want OriginalID %q", got, "origabc12345")
		}
		// Fallback when OriginalID is unset (legacy fixtures bypassing
		// handleWebSocket).
		c2 := &Client{ID: "onlyid1234567"}
		if got := c2.rateLimitKey(); got != "onlyid1234567" {
			t.Errorf("rateLimitKey with empty OriginalID = %q, want c.ID %q", got, "onlyid1234567")
		}
	})

	t.Run("cycling c.ID shares one bucket and does not refresh quota", func(t *testing.T) {
		cfg := &config.Config{
			SessionTimeout:  time.Hour,
			CleanupInterval: time.Hour,
			PingInterval:    time.Second,
		}
		h := NewHub(cfg)
		h.Run()
		defer h.Shutdown()

		c := &Client{
			ID:         "origabc12345",
			OriginalID: "origabc12345",
			Hub:        h,
			Send:       make(chan []byte, 20),
			IP:         "127.0.0.1",
		}

		// Seed the stable bucket. The rate limiter allows a burst then
		// throttles to MaxMessagesPerSecond once messageCount reaches
		// that threshold within the 1s window (see CheckMessageRate).
		if !h.Security.CheckMessageRate(c.rateLimitKey()) {
			t.Fatal("first message should be allowed")
		}
		if n := h.Security.MessageRateEntryCount(); n != 1 {
			t.Errorf("after first message: %d rate buckets, want 1 (keyed by OriginalID)", n)
		}

		// Simulate handleStagiaireJoin overwriting c.ID with a presented
		// stale ID (client.go:458). The next message must land in the
		// SAME bucket (OriginalID), not mint a fresh one under the
		// cycled ID. Before R22 this seeded a new MaxBurstMessages
		// budget and leaked the prior entry.
		c.ID = "staleabcdef1"
		if !h.Security.CheckMessageRate(c.rateLimitKey()) {
			t.Error("second message against the stable bucket should be allowed")
		}
		if n := h.Security.MessageRateEntryCount(); n != 1 {
			t.Errorf("after ID cycle: %d rate buckets, want 1 (no leak, no fresh bucket)", n)
		}
		if h.Security.HasMessageRateEntry("staleabcdef1") {
			t.Error("no bucket should exist under the cycled (mutable) c.ID")
		}

		// Exhaust the steady-state rate (MaxMessagesPerSecond messages in
		// a tight loop) so the next call is throttled. Then cycle the ID
		// again and confirm the throttle still holds — cycling must not
		// refresh the quota.
		for i := 0; i < security.MaxMessagesPerSecond; i++ {
			h.Security.CheckMessageRate(c.rateLimitKey())
		}
		if h.Security.CheckMessageRate(c.rateLimitKey()) {
			t.Error("bucket should be throttled after MaxMessagesPerSecond tight messages")
		}
		c.ID = "againabcdef1"
		if h.Security.CheckMessageRate(c.rateLimitKey()) {
			t.Error("cycling c.ID must NOT refresh the throttle (R22 quota refresh)")
		}

		// Disconnect cleanup (readPump's defer) removes the single bucket.
		h.Security.RemoveMessageRate(c.rateLimitKey())
		if n := h.Security.MessageRateEntryCount(); n != 0 {
			t.Errorf("after disconnect cleanup: %d rate buckets, want 0 (entry removed)", n)
		}
	})
}

// goroutinesIn returns the count of live goroutines whose current stack
// contains fnName. Used by the R24 writePump-exit test to detect a
// lingering writePump deterministically (rather than the noisy absolute
// runtime.NumGoroutine count that includes GC/finalizer goroutines).
func goroutinesIn(fnName string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), fnName)
		}
		buf = make([]byte, len(buf)*2)
	}
}

// TestPendingSendFlushNoOpAfterDone covers R24: once done is closed
// (readPump exited), a pendingSend.flush must be a clean no-op — no
// delivery to Send (no live writePump to drain it), no panic, no block.
// Without the done case in flush's select, the message would buffer into
// the dead Send channel forever or log a misleading "buffer full" warning.
func TestPendingSendFlushNoOpAfterDone(t *testing.T) {
	c := &Client{
		Send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	close(c.done)

	p := pendingSend{client: c, msg: map[string]any{"type": "test"}}
	p.flush()

	select {
	case msg := <-c.Send:
		t.Errorf("flush must not deliver to Send after done is closed (R24), got %s", msg)
	default:
	}
}

// TestPendingSendFlushDeliversWhenDoneOpen confirms the done case
// doesn't suppress normal delivery: when done is still open and the
// buffer has room, flush must deliver to Send as before.
func TestPendingSendFlushDeliversWhenDoneOpen(t *testing.T) {
	c := &Client{
		Send: make(chan []byte, 2),
		done: make(chan struct{}),
	}
	p := pendingSend{client: c, msg: map[string]any{"type": "ok"}}
	p.flush()

	select {
	case msg := <-c.Send:
		var resp map[string]any
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["type"] != "ok" {
			t.Errorf("delivered wrong message: %v", resp["type"])
		}
	default:
		t.Error("flush must deliver when done is open and buffer has room")
	}
}

// TestWritePumpExitsPromptlyOnEviction is the R24 integration test.
//
// Before R24, writePump's only exit paths were a write error, a ping
// error (up to PingInterval away), or ctx cancel (shutdown). On a
// per-client eviction (slow-buffer disconnect) or trainer takeover,
// only markClosing + Conn.Close ran — writePump stayed parked on its
// select with an empty Send buffer and only woke on the next pingTick.C
// where the ping write failed on the dead conn. With PingInterval = 30s
// (the default) that is a 30s goroutine + 512-entry Send-buffer linger
// per evicted client. Under a reconnect storm at the resource caps
// (200 stagiaires × 1000 sessions) this was a real transient spike.
//
// The fix adds case <-c.done to writePump's select; done is closed in
// readPump's defer (LIFO: after Conn.Close). This test wires a real WS
// connection, evicts it, and asserts writePump's goroutine exits within
// a short bound — not PingInterval. Without R24 (PingInterval = 1h in
// this test) the writePump goroutine would linger for up to 1 hour and
// the test would time out.
func TestWritePumpExitsPromptlyOnEviction(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Hour, // without R24, writePump lingers up to 1h
		WriteTimeout:    time.Second,
		AllowedOrigins:  []string{"*"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		clientID, ok := h.GenerateUniqueClientID()
		if !ok {
			conn.Close()
			return
		}
		c := NewClient(h, conn, "127.0.0.1", clientID)
		c.Type = "trainer"
		c.SessionID = "EVT" // valid 3-letter code from the safe alphabet
		select {
		case h.Register <- c:
		case <-time.After(time.Second):
			conn.Close()
			return
		}
		c.Start()
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]

	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	wsConn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer wsConn.Close()

	// Wait for the hub to register the client and start both pumps.
	deadline := time.Now().Add(2 * time.Second)
	var serverClient *Client
	for time.Now().Before(deadline) {
		h.mu.RLock()
		if conns, ok := h.Connections["EVT"]; ok && conns.Trainer != nil {
			serverClient = conns.Trainer
		}
		h.mu.RUnlock()
		if serverClient != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if serverClient == nil {
		t.Fatal("client was not registered within 2s")
	}

	// Both pumps must be live at this point.
	if n := goroutinesIn("writePump"); n != 1 {
		t.Fatalf("precondition: expected 1 writePump goroutine, got %d", n)
	}
	if n := goroutinesIn("readPump"); n != 1 {
		t.Fatalf("precondition: expected 1 readPump goroutine, got %d", n)
	}

	// Pump the client-side read so we observe the server-side close
	// (otherwise the conn lingers in a TCP close-wait state).
	go func() {
		for {
			if _, _, err := wsConn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Evict: markClosing + Conn.Close — the same path as trySend's
	// slow-buffer eviction (client.go) and reconnect-by-ID takeover
	// (hub.go).
	serverClient.markClosing()
	serverClient.Conn.Close()

	// With R24: readPump exits (ReadMessage errors on closed conn) →
	// defer closes done → writePump's case <-c.done fires → writePump
	// exits. Both goroutines gone within milliseconds.
	//
	// Without R24: readPump exits but writePump lingers parked on its
	// select — its only wake is pingTick.C at PingInterval (1 hour)
	// where the ping write fails on the dead conn.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if goroutinesIn("writePump") == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := goroutinesIn("writePump"); n != 0 {
		t.Errorf("writePump did not exit within 5s of eviction (R24): %d goroutine(s) still live "+
			"(without R24 it would linger up to PingInterval=%v)", n, cfg.PingInterval)
	}
}

// TestClientIdentityAtomicityRace exercises the R25 data race: c.ID / c.Type
// / c.SessionID / c.Name are written in readPump (handleStagiaireJoin) but
// read cross-goroutine by logAttrs (writePump write errors, BroadcastSession
// fanout via trySend). Before the atomic.Pointer[clientIdentity] fix, Go's
// non-atomic string assignment triggered a -race detector report. This test
// runs the writer and reader concurrently under -race and asserts no report.
func TestClientIdentityAtomicityRace(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Hour,
		WriteTimeout:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu"},
	}
	h := NewHub(cfg)
	h.Run()
	defer h.Shutdown()

	c := NewClient(h, nil, "127.0.0.1", "orig-id-12345")
	c.Send = make(chan []byte, 1) // tiny buffer so trySend's default branch fires

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer goroutine: simulates readPump cycling IDs/Type/SessionID/Name
	// via stagiaire_join messages (the mutation sites at client.go:520, 523,
	// 526, 527, 555, 841).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			c.ID = fmt.Sprintf("id-%d", i)
			c.Type = "stagiaire"
			c.SessionID = "ABC"
			c.Name = fmt.Sprintf("name-%d", i)
			c.snapshotIdentity()
		}
	}()

	// Reader goroutine: simulates writePump / BroadcastSession fanout
	// calling logAttrs and trySend on a client whose identity is being
	// mutated by its own readPump.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// logAttrs reads ID/SessionID/Type cross-goroutine.
			_ = c.logAttrs()
			// trySend's default branch reads Type cross-goroutine.
			c.trySend([]byte("{}"))
			// Drain the channel so the buffer doesn't fill and we
			// alternate between the send and default paths.
			select {
			case <-c.Send:
			default:
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
