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
	go h.Run()
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

func TestCompetitiveRevealFlow(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Second,
		ValidColors:     []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "gris"},
	}
	h := NewHub(cfg)
	go h.Run()
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
	go h.Run()
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
	go h.Run()
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
	go h.Run()
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
