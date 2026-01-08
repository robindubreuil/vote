package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestGenerateID vérifie que les IDs générés sont uniques
func TestGenerateID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ids := make(map[string]bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		id := generateID()

		// Vérifier la longueur
		if len(id) != 12 {
			t.Errorf("Expected ID length 12, got %d", len(id))
		}

		// Vérifier que l'ID ne contient que des caractères valides
		for _, c := range id {
			if !strings.Contains("abcdefghijklmnopqrstuvwxyz0123456789", string(c)) {
				t.Errorf("Invalid character in ID: %c", c)
			}
		}

		// Vérifier l'unicité
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}

	if len(ids) != iterations {
		t.Errorf("Expected %d unique IDs, got %d", iterations, len(ids))
	}
}

// TestGenerateIDFormat vérifie le format des IDs générés
func TestGenerateIDFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	id := generateID()

	if len(id) != 12 {
		t.Errorf("Expected ID length 12, got %d", len(id))
	}

	// Vérifier que tous les caractères sont alphanumériques minuscules
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			t.Errorf("Invalid character in ID: %c (should be a-z or 0-9)", c)
		}
	}
}

// TestMessageParsing vérifie le parsing des messages
func TestMessageParsing(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
		check   func(Message) bool
	}{
		{
			name:    "trainer_join valid",
			json:    `{"type":"trainer_join","sessionCode":"1234","trainerId":"trainer1"}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "trainer_join" &&
					m.SessionCode == "1234" &&
					m.TrainerID == "trainer1"
			},
		},
		{
			name:    "stagiaire_join valid",
			json:    `{"type":"stagiaire_join","sessionCode":"1234","stagiaireId":"stagiaire1"}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "stagiaire_join" &&
					m.SessionCode == "1234" &&
					m.StagiaireID == "stagiaire1"
			},
		},
		{
			name:    "start_vote valid",
			json:    `{"type":"start_vote","colors":["rouge","vert","bleu"],"multipleChoice":false}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "start_vote" &&
					len(m.Colors) == 3 &&
					m.MultipleChoice == false
			},
		},
		{
			name:    "vote valid single choice",
			json:    `{"type":"vote","stagiaireId":"stagiaire1","couleurs":["rouge"]}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "vote" &&
					m.StagiaireID == "stagiaire1" &&
					len(m.Couleurs) == 1 &&
					m.Couleurs[0] == "rouge"
			},
		},
		{
			name:    "vote valid multiple choice",
			json:    `{"type":"vote","stagiaireId":"stagiaire1","couleurs":["rouge","vert","bleu"]}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "vote" &&
					len(m.Couleurs) == 3
			},
		},
		{
			name:    "close_vote valid",
			json:    `{"type":"close_vote"}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "close_vote"
			},
		},
		{
			name:    "reset_vote valid",
			json:    `{"type":"reset_vote","colors":["rouge","vert"],"multipleChoice":true}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "reset_vote" &&
					len(m.Colors) == 2 &&
					m.MultipleChoice == true
			},
		},
		{
			name:    "invalid JSON",
			json:    `{invalid json}`,
			wantErr: true,
			check:   nil,
		},
		{
			name:    "missing type",
			json:    `{"sessionCode":"1234"}`,
			wantErr: false, // JSON is valid, just missing type
			check: func(m Message) bool {
				return m.Type == ""
			},
		},
		{
			name:    "empty colors array",
			json:    `{"type":"start_vote","colors":[],"multipleChoice":false}`,
			wantErr: false,
			check: func(m Message) bool {
				return m.Type == "start_vote" && len(m.Colors) == 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg Message
			err := json.Unmarshal([]byte(tt.json), &msg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil && !tt.wantErr {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.check != nil && !tt.check(msg) {
				t.Errorf("Message validation failed")
			}
		})
	}
}

// TestMessageSerialization vérifie la sérialisation des messages
func TestMessageSerialization(t *testing.T) {
	tests := []struct {
		name  string
		msg   Message
		check func(string) bool
	}{
		{
			name: "vote_started",
			msg: Message{
				Type:           "vote_started",
				Colors:         []string{"rouge", "vert", "bleu"},
				MultipleChoice: false,
			},
			check: func(s string) bool {
				// Vérifier que le JSON contient les éléments attendus
				return strings.Contains(s, "vote_started") &&
					strings.Contains(s, "rouge") &&
					strings.Contains(s, "colors")
			},
		},
		{
			name: "vote_received",
			msg: Message{
				Type:        "vote_received",
				StagiaireID: "stagiaire1",
				Couleurs:    []string{"rouge", "bleu"},
			},
			check: func(s string) bool {
				return strings.Contains(s, "vote_received") &&
					strings.Contains(s, "stagiaire1") &&
					strings.Contains(s, "couleurs")
			},
		},
		{
			name: "session_created",
			msg: Message{
				Type:        "session_created",
				SessionCode: "1234",
			},
			check: func(s string) bool {
				return strings.Contains(s, "session_created") &&
					strings.Contains(s, "1234")
			},
		},
		{
			name: "vote_closed",
			msg: Message{
				Type: "vote_closed",
			},
			check: func(s string) bool {
				return strings.Contains(s, "vote_closed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Errorf("Failed to marshal message: %v", err)
				return
			}

			if !tt.check(string(data)) {
				t.Errorf("Serialized message doesn't match expected format: %s", string(data))
			}
		})
	}
}

// TestColorNames vérifie que les noms de couleurs sont valides
func TestColorNames(t *testing.T) {
	validColors := map[string]bool{
		"rouge":   true,
		"vert":    true,
		"bleu":    true,
		"jaune":   true,
		"orange":  true,
		"violet":  true,
		"rose":    true,
		"gris":    true,
	}

	// Test de message avec couleurs valides
	msg := `{"type":"vote","couleurs":["rouge","vert","bleu"]}`
	var m Message
	err := json.Unmarshal([]byte(msg), &m)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	for _, color := range m.Couleurs {
		if !validColors[color] {
			t.Errorf("Invalid color: %s", color)
		}
	}
}

// TestEmptyVote vérifie le vote sans sélection
func TestEmptyVote(t *testing.T) {
	msg := `{"type":"vote","stagiaireId":"stagiaire1","couleurs":[]}`
	var m Message
	err := json.Unmarshal([]byte(msg), &m)
	if err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if len(m.Couleurs) != 0 {
		t.Errorf("Expected empty couleurs array, got %v", m.Couleurs)
	}
}

// BenchmarkGenerateID benchmark la génération d'IDs
func BenchmarkGenerateID(b *testing.B) {
	gin.SetMode(gin.TestMode)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		generateID()
	}
}

// BenchmarkJSONMarshal benchmark la sérialisation JSON
func BenchmarkJSONMarshal(b *testing.B) {
	msg := map[string]interface{}{
		"type":           "vote_started",
		"colors":         []string{"rouge", "vert", "bleu", "jaune"},
		"multipleChoice": false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(msg)
	}
}

// BenchmarkJSONUnmarshal benchmark le parsing JSON
func BenchmarkJSONUnmarshal(b *testing.B) {
	data := []byte(`{"type":"vote","stagiaireId":"stagiaire1","couleurs":["rouge","vert"]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var msg Message
		_ = json.Unmarshal(data, &msg)
	}
}

// TestFallbackGenerateID vérifie la génération d'ID de secours
func TestFallbackGenerateID(t *testing.T) {
	id := fallbackGenerateID()

	if len(id) != 12 {
		t.Errorf("Expected ID length 12, got %d", len(id))
	}

	// Vérifier que tous les caractères sont alphanumériques minuscules
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			t.Errorf("Invalid character in fallback ID: %c", c)
		}
	}
}

// TestFallbackGenerateIDUniqueness vérifie que les fallback IDs sont uniques
func TestFallbackGenerateIDUniqueness(t *testing.T) {
	ids := make(map[string]bool)
	iterations := 100

	// Attendre un peu entre chaque génération pour éviter les doublons
	for i := 0; i < iterations; i++ {
		id := fallbackGenerateID()
		if ids[id] {
			t.Errorf("Duplicate fallback ID generated: %s", id)
		}
		ids[id] = true
	}
}

// TestHandleTrainerJoin vérifie la gestion de la connexion formateur
func TestHandleTrainerJoin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un client test
	client := &Client{
		ID:        "trainer1",
		Type:      "",
		SessionID: "",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Drainer le channel
	go func() {
		for range client.Send {
		}
	}()

	msg := Message{
		Type:        "trainer_join",
		SessionCode: "TEST01",
		TrainerID:   "trainer1",
	}

	client.handleTrainerJoin(msg)

	// Vérifier que le client a été configuré
	if client.Type != "trainer" {
		t.Errorf("Expected client type 'trainer', got '%s'", client.Type)
	}

	if client.SessionID != "TEST01" {
		t.Errorf("Expected session ID 'TEST01', got '%s'", client.SessionID)
	}

	// Vérifier que la session a été créée
	time.Sleep(50 * time.Millisecond)
	session := hub.GetSession("TEST01")
	if session == nil {
		t.Fatal("Session should be created")
	}

	if session.Trainer == nil {
		t.Fatal("Session should have a trainer")
	}
}

// TestHandleStagiaireJoin vérifie la gestion de la connexion stagiaire
func TestHandleStagiaireJoin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer d'abord un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST02",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Créer un client stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "",
		SessionID: "",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Drainer le channel
	go func() {
		for range stagiaire.Send {
		}
	}()

	msg := Message{
		Type:        "stagiaire_join",
		SessionCode: "TEST02",
		StagiaireID: "stagiaire1",
	}

	stagiaire.handleStagiaireJoin(msg)

	// Vérifier que le client a été configuré
	if stagiaire.Type != "stagiaire" {
		t.Errorf("Expected client type 'stagiaire', got '%s'", stagiaire.Type)
	}

	if stagiaire.SessionID != "TEST02" {
		t.Errorf("Expected session ID 'TEST02', got '%s'", stagiaire.SessionID)
	}

	if stagiaire.ID != "stagiaire1" {
		t.Errorf("Expected ID 'stagiaire1', got '%s'", stagiaire.ID)
	}
}

// TestHandleStagiaireJoinNonExistent vérifie l'erreur quand la session n'existe pas
func TestHandleStagiaireJoinNonExistent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "",
		SessionID: "",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	msg := Message{
		Type:        "stagiaire_join",
		SessionCode: "NONEXISTENT",
		StagiaireID: "stagiaire1",
	}

	stagiaire.handleStagiaireJoin(msg)

	// Vérifier qu'un message d'erreur a été envoyé
	select {
	case data := <-stagiaire.Send:
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		if result["type"] != "join_error" {
			t.Errorf("Expected type 'join_error', got %v", result["type"])
		}
	default:
		t.Error("Expected error message to be sent")
	}
}

// TestHandleStartVote vérifie le démarrage d'un vote
func TestHandleStartVote(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST03",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	msg := Message{
		Type:           "start_vote",
		Colors:         []string{"rouge", "vert", "bleu"},
		MultipleChoice: false,
	}

	trainer.handleStartVote(msg)

	// Vérifier que l'état du vote a été mis à jour
	session := hub.GetSession("TEST03")
	if session == nil {
		t.Fatal("Session should exist")
	}

	if session.VoteState != "active" {
		t.Errorf("Expected vote state 'active', got '%s'", session.VoteState)
	}

	if len(session.ActiveColors) != 3 {
		t.Errorf("Expected 3 active colors, got %d", len(session.ActiveColors))
	}

	if session.MultipleChoice != false {
		t.Error("Expected multiple choice to be false")
	}
}

// TestHandleStartVoteNotTrainer vérifie qu'un stagiaire ne peut pas démarrer un vote
func TestHandleStartVoteNotTrainer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST04",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Créer un stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "stagiaire",
		SessionID: "TEST04",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	msg := Message{
		Type:           "start_vote",
		Colors:         []string{"rouge", "vert"},
		MultipleChoice: true,
	}

	// Le stagiaire essaie de démarrer un vote
	stagiaire.handleStartVote(msg)

	// L'état ne devrait pas changer
	session := hub.GetSession("TEST04")
	if session.VoteState != "idle" {
		t.Errorf("Vote state should remain 'idle', got '%s'", session.VoteState)
	}
}

// TestHandleVote vérifie l'enregistrement d'un vote
func TestHandleVote(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST05",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote
	session := hub.GetSession("TEST05")
	session.VoteState = "active"
	session.Votes = make(map[string][]string)

	// Créer un stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "stagiaire",
		SessionID: "TEST05",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Drainer le channel
	go func() {
		for range stagiaire.Send {
		}
	}()

	// Drainer le channel du formateur
	go func() {
		for range trainer.Send {
		}
	}()

	msg := Message{
		Type:        "vote",
		StagiaireID: "stagiaire1",
		Couleurs:    []string{"rouge"},
	}

	stagiaire.handleVote(msg)

	// Vérifier que le vote a été enregistré
	vote, exists := session.Votes["stagiaire1"]
	if !exists {
		t.Fatal("Vote should be recorded")
	}

	if len(vote) != 1 || vote[0] != "rouge" {
		t.Errorf("Expected vote for 'rouge', got %v", vote)
	}
}

// TestHandleVoteMultipleChoice vérifie un vote avec choix multiple
func TestHandleVoteMultipleChoice(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST06",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote
	session := hub.GetSession("TEST06")
	session.VoteState = "active"
	session.Votes = make(map[string][]string)

	// Créer un stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "stagiaire",
		SessionID: "TEST06",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Drainer les channels
	go func() {
		for range stagiaire.Send {
		}
	}()
	go func() {
		for range trainer.Send {
		}
	}()

	msg := Message{
		Type:        "vote",
		StagiaireID: "stagiaire1",
		Couleurs:    []string{"rouge", "bleu", "jaune"},
	}

	stagiaire.handleVote(msg)

	// Vérifier que le vote a été enregistré
	vote, exists := session.Votes["stagiaire1"]
	if !exists {
		t.Fatal("Vote should be recorded")
	}

	if len(vote) != 3 {
		t.Errorf("Expected 3 colors, got %d", len(vote))
	}
}

// TestHandleVoteUpdate vérifie la mise à jour d'un vote existant
func TestHandleVoteUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST07",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote
	session := hub.GetSession("TEST07")
	session.VoteState = "active"
	session.Votes = make(map[string][]string)
	session.Votes["stagiaire1"] = []string{"rouge"}

	// Créer un stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "stagiaire",
		SessionID: "TEST07",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Drainer les channels
	go func() {
		for range stagiaire.Send {
		}
	}()
	go func() {
		for range trainer.Send {
		}
	}()

	msg := Message{
		Type:        "vote",
		StagiaireID: "stagiaire1",
		Couleurs:    []string{"vert"},
	}

	stagiaire.handleVote(msg)

	// Vérifier que le vote a été mis à jour
	vote := session.Votes["stagiaire1"]
	if len(vote) != 1 || vote[0] != "vert" {
		t.Errorf("Expected updated vote for 'vert', got %v", vote)
	}
}

// TestHandleCloseVote vérifie la fermeture d'un vote
func TestHandleCloseVote(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST08",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote
	session := hub.GetSession("TEST08")
	session.VoteState = "active"

	msg := Message{}

	trainer.handleCloseVote(msg)

	// Vérifier que l'état a été mis à jour
	if session.VoteState != "closed" {
		t.Errorf("Expected vote state 'closed', got '%s'", session.VoteState)
	}
}

// TestHandleCloseVoteNotTrainer vérifie qu'un stagiaire ne peut pas fermer un vote
func TestHandleCloseVoteNotTrainer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST09",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Créer un stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "stagiaire",
		SessionID: "TEST09",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote
	session := hub.GetSession("TEST09")
	session.VoteState = "active"

	msg := Message{}

	// Le stagiaire essaie de fermer le vote
	stagiaire.handleCloseVote(msg)

	// L'état ne devrait pas changer
	if session.VoteState != "active" {
		t.Errorf("Vote state should remain 'active', got '%s'", session.VoteState)
	}
}

// TestHandleResetVote vérifie la réinitialisation d'un vote
func TestHandleResetVote(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST10",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote avec des données
	session := hub.GetSession("TEST10")
	session.VoteState = "closed"
	session.ActiveColors = []string{"rouge", "vert"}
	session.MultipleChoice = false
	session.Votes = make(map[string][]string)
	session.Votes["stagiaire1"] = []string{"rouge"}

	msg := Message{
		Colors:         []string{"bleu", "jaune", "orange"},
		MultipleChoice: true,
	}

	trainer.handleResetVote(msg)

	// Vérifier que l'état a été réinitialisé
	if session.VoteState != "idle" {
		t.Errorf("Expected vote state 'idle', got '%s'", session.VoteState)
	}

	if len(session.ActiveColors) != 3 {
		t.Errorf("Expected 3 active colors, got %d", len(session.ActiveColors))
	}

	if session.MultipleChoice != true {
		t.Error("Expected multiple choice to be true")
	}

	if len(session.Votes) != 0 {
		t.Errorf("Expected votes to be cleared, got %d votes", len(session.Votes))
	}
}

// TestHandleResetVoteNotTrainer vérifie qu'un stagiaire ne peut pas réinitialiser un vote
func TestHandleResetVoteNotTrainer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	go hub.Run()

	// Créer un formateur
	trainer := &Client{
		ID:        "trainer1",
		Type:      "trainer",
		SessionID: "TEST11",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Créer un stagiaire
	stagiaire := &Client{
		ID:        "stagiaire1",
		Type:      "stagiaire",
		SessionID: "TEST11",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}
	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	// Initialiser le vote
	session := hub.GetSession("TEST11")
	session.VoteState = "active"

	msg := Message{
		Colors:         []string{"rouge"},
		MultipleChoice: false,
	}

	// Le stagiaire essaie de réinitialiser le vote
	stagiaire.handleResetVote(msg)

	// L'état ne devrait pas changer
	if session.VoteState != "active" {
		t.Errorf("Vote state should remain 'active', got '%s'", session.VoteState)
	}
}

// TestHandleMessageUnknownType vérifie le gestionnaire de messages avec type inconnu
func TestHandleMessageUnknownType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	client := &Client{
		ID:        "test1",
		SessionID: "TEST12",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Message avec type inconnu (juste pour vérifier qu'il n'y a pas de panic)
	data := []byte(`{"type":"unknown_type","data":"test"}`)

	// Ne devrait pas causer de panic
	client.handleMessage(data)
}

// TestHandleMessageInvalidJSON vérifie le gestionnaire avec JSON invalide
func TestHandleMessageInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	client := &Client{
		ID:        "test1",
		SessionID: "TEST13",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Message avec JSON invalide
	data := []byte(`{invalid json}`)

	// Ne devrait pas causer de panic
	client.handleMessage(data)
}

// TestHandleMessageEmpty vérifie le gestionnaire avec un message vide
func TestHandleMessageEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	client := &Client{
		ID:        "test1",
		SessionID: "TEST14",
		Send:      make(chan []byte, 256),
		Hub:       hub,
	}

	// Message vide
	data := []byte(``)

	// Ne devrait pas causer de panic
	client.handleMessage(data)
}

// TestSendJSONFullChannel vérifie l'envoi JSON quand le channel est plein
func TestSendJSONFullChannel(t *testing.T) {
	client := &Client{
		ID:        "test1",
		SessionID: "1234",
		Send:      make(chan []byte, 1),
		Hub:       NewHub(),
	}

	// Remplir le channel
	client.Send <- []byte("full")

	// Essayer d'envoyer un autre message (ne devrait pas bloquer)
	err := sendJSON(client, map[string]interface{}{
		"type": "test",
	})

	// La fonction ne retourne pas d'erreur même si le channel est plein
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}
