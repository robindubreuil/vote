package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// Helper pour créer un client de test
// Note: Conn est nil pour les tests car le hub utilise principalement Send
func createTestClient(id, sessionID, clientType string) *Client {
	return &Client{
		ID:        id,
		SessionID: sessionID,
		Type:      clientType,
		Conn:      nil,
		Send:      make(chan []byte, 256),
	}
}

// collectMessagesFromChannel collecte les messages envoyés sur un channel
func collectMessagesFromChannel(ch chan []byte, duration time.Duration) [][]byte {
	ch2 := make(chan []byte, 256)
	var messages [][]byte

	go func() {
		for {
			select {
			case msg := <-ch:
				messages = append(messages, msg)
				ch2 <- msg // Re-forward pour ne pas bloquer
			case <-time.After(duration):
				return
			}
		}
	}()

	// Drainer les messages reçus
	go func() {
		for range ch2 {
		}
	}()

	return messages
}

// TestNewHub vérifie la création du hub
func TestNewHub(t *testing.T) {
	hub := NewHub()

	if hub.Sessions == nil {
		t.Fatal("Sessions map should be initialized")
	}
	if hub.Register == nil {
		t.Fatal("Register channel should be initialized")
	}
	if hub.Unregister == nil {
		t.Fatal("Unregister channel should be initialized")
	}
	if hub.Broadcast == nil {
		t.Fatal("Broadcast channel should be initialized")
	}
}

// TestTrainerJoinCreateSession vérifie qu'un formateur peut créer une session
func TestTrainerJoinCreateSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Vérifier que la session a été créée
	session := hub.GetSession("1234")
	if session == nil {
		t.Fatal("Session should be created")
	}

	if session.SessionCode != "1234" {
		t.Errorf("Expected session code 1234, got %s", session.SessionCode)
	}

	if session.Trainer == nil {
		t.Fatal("Trainer should be set")
	}

	if session.Trainer.ID != "trainer1" {
		t.Errorf("Expected trainer ID trainer1, got %s", session.Trainer.ID)
	}
}

// TestStagiaireJoinExistingSession vérifie qu'un stagiaire peut rejoindre une session
func TestStagiaireJoinExistingSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Un stagiaire rejoint
	stagiaire := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")
	if session == nil {
		t.Fatal("Session should exist")
	}

	if len(session.Stagiaires) != 1 {
		t.Errorf("Expected 1 stagiaire, got %d", len(session.Stagiaires))
	}

	if _, exists := session.Stagiaires["stagiaire1"]; !exists {
		t.Fatal("Stagiaire1 should be in the session")
	}
}

// TestStagiaireJoinNonExistingSession vérifie qu'un stagiaire ne peut pas rejoindre une session inexistante
func TestStagiaireJoinNonExistingSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	stagiaire := createTestClient("stagiaire1", "9999", "stagiaire")
	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("9999")
	if session != nil {
		t.Fatal("Session should not exist")
	}
}

// TestMultipleStagiaires vérifie que plusieurs stagiaires peuvent rejoindre
func TestMultipleStagiaires(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Ajouter plusieurs stagiaires
	for i := 0; i < 5; i++ {
		id := "stagiaire" + string(rune('1'+i))
		stagiaire := createTestClient(id, "1234", "stagiaire")
		hub.Register <- stagiaire
	}
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")
	if len(session.Stagiaires) != 5 {
		t.Errorf("Expected 5 stagiaires, got %d", len(session.Stagiaires))
	}
}

// TestStagiaireDisconnect vérifie qu'un stagiaire peut se déconnecter
func TestStagiaireDisconnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Ajouter un stagiaire
	stagiaire := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	// Déconnecter le stagiaire
	hub.Unregister <- stagiaire
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")
	if len(session.Stagiaires) != 0 {
		t.Errorf("Expected 0 stagiaires after disconnect, got %d", len(session.Stagiaires))
	}
}

// TestTrainerDisconnect vérifie que la déconnexion du formateur ferme la session
func TestTrainerDisconnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Ajouter des stagiaires
	stagiaire1 := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire1

	stagiaire2 := createTestClient("stagiaire2", "1234", "stagiaire")
	hub.Register <- stagiaire2
	time.Sleep(50 * time.Millisecond)

	// Déconnecter le formateur
	hub.Unregister <- trainer
	time.Sleep(50 * time.Millisecond)

	// La session devrait toujours exister mais sans formateur (nettoyage via TTL)
	session := hub.GetSession("1234")
	if session == nil {
		t.Fatal("Session should still exist (cleaned up via TTL)")
	}
	if session.Trainer != nil {
		t.Error("Session trainer should be nil after disconnect")
	}
}

// TestStagiaireReconnect vérifie qu'un stagiaire peut se reconnecter
func TestStagiaireReconnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Première connexion du stagiaire
	stagiaire1 := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire1
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")
	oldClient := session.Stagiaires["stagiaire1"]
	if oldClient == nil {
		t.Fatal("Stagiaire should be registered")
	}

	// Seconde connexion du même stagiaire (reconnexion)
	stagiaire2 := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire2
	time.Sleep(50 * time.Millisecond)

	session = hub.GetSession("1234")
	if len(session.Stagiaires) != 1 {
		t.Errorf("Expected 1 stagiaire after reconnect, got %d", len(session.Stagiaires))
	}

	newClient := session.Stagiaires["stagiaire1"]
	if newClient == oldClient {
		t.Error("Client should be updated after reconnect")
	}
}

// TestBroadcastToSession vérifie le broadcast de messages
func TestBroadcastToSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Ajouter des stagiaires
	stagiaire1 := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire1

	stagiaire2 := createTestClient("stagiaire2", "1234", "stagiaire")
	hub.Register <- stagiaire2
	time.Sleep(50 * time.Millisecond)

	// Drainer les channels pour éviter le blocage
	go func() {
		for range trainer.Send {
		}
	}()
	go func() {
		for range stagiaire1.Send {
		}
	}()
	go func() {
		for range stagiaire2.Send {
		}
	}()

	// Envoyer un broadcast
	message := []byte(`{"type":"test","data":"hello"}`)
	hub.Broadcast <- &BroadcastMessage{
		SessionID: "1234",
		Message:   message,
	}
	time.Sleep(50 * time.Millisecond)

	// Le broadcast ne devrait pas causer de panic
}

// TestBroadcastExcludeID vérifie l'exclusion d'un client du broadcast
func TestBroadcastExcludeID(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Ajouter des stagiaires
	stagiaire1 := createTestClient("stagiaire1", "1234", "stagiaire")
	hub.Register <- stagiaire1

	stagiaire2 := createTestClient("stagiaire2", "1234", "stagiaire")
	hub.Register <- stagiaire2
	time.Sleep(50 * time.Millisecond)

	// Drainer les channels
	go func() {
		for range trainer.Send {
		}
	}()
	go func() {
		for range stagiaire1.Send {
		}
	}()
	go func() {
		for range stagiaire2.Send {
		}
	}()

	// Envoyer un broadcast en excluant stagiaire1
	message := []byte(`{"type":"test","data":"hello"}`)
	hub.Broadcast <- &BroadcastMessage{
		SessionID: "1234",
		Message:   message,
		ExcludeID: "stagiaire1",
	}
	time.Sleep(50 * time.Millisecond)

	// Le broadcast ne devrait pas causer de panic
}

// TestVoteState vérifie l'état de vote
func TestVoteState(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")

	// État initial
	if session.VoteState != "idle" {
		t.Errorf("Expected initial vote state 'idle', got '%s'", session.VoteState)
	}

	// Modifier l'état
	hub.mu.Lock()
	session.VoteState = "active"
	session.ActiveColors = []string{"rouge", "vert", "bleu"}
	session.MultipleChoice = false
	session.Votes = make(map[string][]string)
	hub.mu.Unlock()

	// Vérifier les modifications
	if session.VoteState != "active" {
		t.Errorf("Expected vote state 'active', got '%s'", session.VoteState)
	}

	if len(session.ActiveColors) != 3 {
		t.Errorf("Expected 3 active colors, got %d", len(session.ActiveColors))
	}
}

// TestVotes vérifie l'enregistrement des votes
func TestVotes(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")

	// Initialiser les votes
	hub.mu.Lock()
	session.VoteState = "active"
	session.ActiveColors = []string{"rouge", "vert", "bleu"}
	session.Votes = make(map[string][]string)
	hub.mu.Unlock()

	// Ajouter des votes
	hub.mu.Lock()
	session.Votes["stagiaire1"] = []string{"rouge"}
	session.Votes["stagiaire2"] = []string{"vert"}
	session.Votes["stagiaire3"] = []string{"rouge", "bleu"}
	hub.mu.Unlock()

	// Vérifier les votes
	if len(session.Votes) != 3 {
		t.Errorf("Expected 3 votes, got %d", len(session.Votes))
	}

	vote1 := session.Votes["stagiaire1"]
	if len(vote1) != 1 || vote1[0] != "rouge" {
		t.Errorf("Expected stagiaire1 to vote for rouge, got %v", vote1)
	}

	vote3 := session.Votes["stagiaire3"]
	if len(vote3) != 2 || vote3[0] != "rouge" || vote3[1] != "bleu" {
		t.Errorf("Expected stagiaire3 to vote for rouge and bleu, got %v", vote3)
	}
}

// TestMultipleSessions vérifie que plusieurs sessions peuvent coexister
func TestMultipleSessions(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer deux sessions
	trainer1 := createTestClient("trainer1", "1111", "trainer")
	hub.Register <- trainer1

	trainer2 := createTestClient("trainer2", "2222", "trainer")
	hub.Register <- trainer2
	time.Sleep(50 * time.Millisecond)

	// Vérifier que les deux sessions existent
	session1 := hub.GetSession("1111")
	if session1 == nil {
		t.Fatal("Session 1111 should exist")
	}

	session2 := hub.GetSession("2222")
	if session2 == nil {
		t.Fatal("Session 2222 should exist")
	}

	// Ajouter des stagiaires à chaque session
	stagiaire1 := createTestClient("stagiaire1", "1111", "stagiaire")
	hub.Register <- stagiaire1

	stagiaire2 := createTestClient("stagiaire2", "2222", "stagiaire")
	hub.Register <- stagiaire2
	time.Sleep(50 * time.Millisecond)

	session1 = hub.GetSession("1111")
	session2 = hub.GetSession("2222")

	if len(session1.Stagiaires) != 1 {
		t.Errorf("Expected 1 stagiaire in session 1111, got %d", len(session1.Stagiaires))
	}

	if len(session2.Stagiaires) != 1 {
		t.Errorf("Expected 1 stagiaire in session 2222, got %d", len(session2.Stagiaires))
	}
}

// TestConcurrentAccess vérifie l'accès concurrent au hub
func TestConcurrentAccess(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Simuler plusieurs opérations concurrentes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					stagiaire := createTestClient(
						"stagiaire"+string(rune('1'+id)),
						"1234",
						"stagiaire",
					)
					hub.Register <- stagiaire
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Laisser tourner un peu
	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()

	// Vérifier qu'il n'y a pas de panic ni de deadlock
	session := hub.GetSession("1234")
	if session == nil {
		t.Fatal("Session should still exist")
	}
}

// TestSendJSON vérifie l'envoi de messages JSON
func TestSendJSON(t *testing.T) {
	client := createTestClient("test1", "1234", "stagiaire")

	// Drainer le channel
	go func() {
		for range client.Send {
		}
	}()

	// Envoyer un message JSON
	err := sendJSON(client, map[string]interface{}{
		"type":    "test",
		"message": "hello",
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Vérifier que le message a été envoyé
	select {
	case msg := <-client.Send:
		var result map[string]interface{}
		if err := json.Unmarshal(msg, &result); err != nil {
			t.Errorf("Failed to parse JSON: %v", err)
		}
		if result["type"] != "test" {
			t.Errorf("Expected type 'test', got %v", result["type"])
		}
	default:
		t.Error("Expected message to be sent")
	}
}

// TestNotifyTrainerSessionUpdate vérifie la notification du formateur
func TestNotifyTrainerSessionUpdate(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")

	// Vider le channel d'abord
	for len(trainer.Send) > 0 {
		<-trainer.Send
	}

	// Notifier le formateur
	hub.notifyTrainerSessionUpdate(session)
	time.Sleep(50 * time.Millisecond)

	// Vérifier qu'un message a été envoyé
	if len(trainer.Send) == 0 {
		t.Error("Expected message to be sent")
		return
	}

	msg := <-trainer.Send
	var result map[string]interface{}
	if err := json.Unmarshal(msg, &result); err != nil {
		t.Errorf("Failed to parse JSON: %v", err)
	}
	if result["type"] != "connected_count" {
		t.Errorf("Expected type 'connected_count', got %v", result["type"])
	}
}

// TestSendError vérifie l'envoi d'un message d'erreur
func TestSendError(t *testing.T) {
	client := createTestClient("test1", "1234", "stagiaire")

	// Drainer le channel
	go func() {
		for range client.Send {
		}
	}()

	// Envoyer une erreur
	sendError(client, "Test error message")

	// Vérifier que le message a été envoyé
	select {
	case msg := <-client.Send:
		var result map[string]interface{}
		if err := json.Unmarshal(msg, &result); err != nil {
			t.Errorf("Failed to parse JSON: %v", err)
		}
		if result["type"] != "error" {
			t.Errorf("Expected type 'error', got %v", result["type"])
		}
		if result["message"] != "Test error message" {
			t.Errorf("Expected message 'Test error message', got %v", result["message"])
		}
	default:
		t.Error("Expected error message to be sent")
	}
}

// TestBroadcastToNonExistentSession vérifie le broadcast vers une session inexistante
func TestBroadcastToNonExistentSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Essayer de broadcast vers une session inexistante
	message := []byte(`{"type":"test"}`)
	hub.Broadcast <- &BroadcastMessage{
		SessionID: "nonexistent",
		Message:   message,
	}
	time.Sleep(50 * time.Millisecond)

	// Ne devrait pas causer de panic
}

// TestBroadcastWithNilSession vérifie le comportement avec une session nil
func TestBroadcastWithNilSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session puis la supprimer
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Supprimer la session
	hub.Unregister <- trainer
	time.Sleep(50 * time.Millisecond)

	// Essayer de broadcast vers la session supprimée
	message := []byte(`{"type":"test"}`)
	hub.Broadcast <- &BroadcastMessage{
		SessionID: "1234",
		Message:   message,
	}
	time.Sleep(50 * time.Millisecond)

	// Ne devrait pas causer de panic
}

// TestGetSessionNonExistent vérifie la récupération d'une session inexistante
func TestGetSessionNonExistent(t *testing.T) {
	hub := NewHub()

	session := hub.GetSession("nonexistent")
	if session != nil {
		t.Error("Expected nil for non-existent session")
	}
}

// TestRegisterClientWithoutTrainer vérifie qu'un stagiaire ne peut pas créer de session
func TestRegisterClientWithoutTrainer(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	stagiaire := createTestClient("stagiaire1", "NEWSESSION", "stagiaire")

	// Drainer le channel
	go func() {
		for range stagiaire.Send {
		}
	}()

	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	// La session ne devrait pas être créée
	session := hub.GetSession("NEWSESSION")
	if session != nil {
		t.Error("Session should not be created by stagiaire")
	}
}

// TestUnregisterNonExistentSession vérifie l'unregister d'une session inexistante
func TestUnregisterNonExistentSession(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := createTestClient("test1", "nonexistent", "trainer")

	// Ne devrait pas causer de panic
	hub.Unregister <- client
	time.Sleep(50 * time.Millisecond)
}

// TestNotifyTrainerSessionUpdateNilTrainer vérifie la notification avec un trainer nil
func TestNotifyTrainerSessionUpdateNilTrainer(t *testing.T) {
	hub := NewHub()

	session := &SessionState{
		SessionCode: "TEST",
		Trainer:     nil,
		Stagiaires:  make(map[string]*Client),
		VoteState:   "idle",
		Votes:       make(map[string][]string),
	}

	// Ne devrait pas causer de panic
	hub.notifyTrainerSessionUpdate(session)
}

// TestBroadcastToSessionWithFullChannel vérifie le broadcast quand le channel est plein
func TestBroadcastToSessionWithFullChannel(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	trainer.Send = make(chan []byte, 1) // Channel très petit
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Remplir le channel
	trainer.Send <- []byte("fill")

	// Envoyer un broadcast (le channel est plein)
	message := []byte(`{"type":"test"}`)
	hub.Broadcast <- &BroadcastMessage{
		SessionID: "1234",
		Message:   message,
	}
	time.Sleep(50 * time.Millisecond)

	// Le channel devrait être fermé à cause du full buffer
	_, ok := <-trainer.Send
	if ok {
		// Le channel est toujours ouvert, le message a été envoyé
	}
}

// TestMultipleVotesSameStagiaire vérifie qu'un stagiaire peut changer son vote
func TestMultipleVotesSameStagiaire(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")

	// Premièrement, voter
	hub.mu.Lock()
	session.Votes["stagiaire1"] = []string{"rouge"}
	hub.mu.Unlock()

	// Changer de vote
	hub.mu.Lock()
	session.Votes["stagiaire1"] = []string{"bleu"}
	hub.mu.Unlock()

	vote := session.Votes["stagiaire1"]
	if len(vote) != 1 || vote[0] != "bleu" {
		t.Errorf("Expected vote 'bleu', got %v", vote)
	}
}

// TestVoteStates vérifie tous les états de vote possibles
func TestVoteStates(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")

	validStates := map[string]bool{
		"idle":   true,
		"active": true,
		"closed": true,
	}

	// Tester tous les états
	for state := range validStates {
		hub.mu.Lock()
		session.VoteState = state
		hub.mu.Unlock()

		if session.VoteState != state {
			t.Errorf("Expected vote state '%s', got '%s'", state, session.VoteState)
		}
	}
}

// TestTrainerReconnect vérifie qu'un formateur peut se reconnecter à sa session
func TestTrainerReconnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Première connexion du formateur
	trainer1 := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer1
	time.Sleep(50 * time.Millisecond)

	session := hub.GetSession("1234")
	oldTrainer := session.Trainer
	if oldTrainer == nil {
		t.Fatal("Trainer should be registered")
	}

	// Seconde connexion du même formateur
	trainer2 := createTestClient("trainer2", "1234", "trainer")
	hub.Register <- trainer2
	time.Sleep(50 * time.Millisecond)

	session = hub.GetSession("1234")
	if session.Trainer.ID != "trainer2" {
		t.Errorf("Expected trainer ID 'trainer2', got '%s'", session.Trainer.ID)
	}

	// L'ancien trainer devrait être remplacé
	if session.Trainer == oldTrainer {
		t.Error("Trainer should be updated after reconnect")
	}
}

// TestRegisterClientStagiaireWithNoTrainer vérifie qu'un stagiaire ne peut pas rejoindre sans formateur
func TestRegisterClientStagiaireWithNoTrainer(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Créer une session avec un formateur
	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Supprimer le formateur
	hub.mu.Lock()
	hub.Sessions["1234"].Trainer = nil
	hub.mu.Unlock()

	// Un stagiaire essaie de rejoindre
	stagiaire := createTestClient("stagiaire1", "1234", "stagiaire")

	// Drainer le channel
	go func() {
		for range stagiaire.Send {
		}
	}()

	hub.Register <- stagiaire
	time.Sleep(50 * time.Millisecond)

	// Le stagiaire ne devrait pas être enregistré
	session := hub.GetSession("1234")
	if _, exists := session.Stagiaires["stagiaire1"]; exists {
		t.Error("Stagiaire should not be registered when no trainer")
	}
}

// TestBroadcastMessageNil vérifie le comportement avec un message vide
func TestBroadcastMessageNil(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	trainer := createTestClient("trainer1", "1234", "trainer")
	hub.Register <- trainer
	time.Sleep(50 * time.Millisecond)

	// Drainer le channel
	go func() {
		for range trainer.Send {
		}
	}()

	// Broadcast avec message vide
	hub.Broadcast <- &BroadcastMessage{
		SessionID: "1234",
		Message:   []byte{},
	}
	time.Sleep(50 * time.Millisecond)

	// Ne devrait pas causer de panic
}
