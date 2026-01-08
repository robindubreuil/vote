package main

import (
	"crypto/rand"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// En production, vérifier l'origine plus strictement
		return true
	},
}

// Message représente un message WebSocket entrant
type Message struct {
	Type           string   `json:"type"`
	SessionCode    string   `json:"sessionCode,omitempty"`
	SessionID      string   `json:"sessionId,omitempty"`
	TrainerID      string   `json:"trainerId,omitempty"`
	StagiaireID    string   `json:"stagiaireId,omitempty"`
	Name           string   `json:"name,omitempty"` // Prénom du stagiaire
	Colors         []string `json:"colors,omitempty"`
	Couleurs       []string `json:"couleurs,omitempty"`
	MultipleChoice bool     `json:"multipleChoice,omitempty"`
}

// HandleWebSocket gère les connexions WebSocket
func (s *Server) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Erreur upgrade WebSocket: %v", err)
		return
	}

	client := &Client{
		ID:   generateID(),
		Conn: conn,
		Send: make(chan []byte, 256),
		Hub:  s.hub,
	}

	// Démarrer la goroutine de lecture
	go client.readPump()
	// Démarrer la goroutine d'écriture
	go client.writePump()
}

// readPump lit les messages du client WebSocket
func (c *Client) readPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512)
	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump écrit les messages au client WebSocket
func (c *Client) writePump() {
	defer c.Conn.Close()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			err := c.Conn.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				log.Printf("Erreur écriture WebSocket: %v", err)
				return
			}
		}
	}
}

// handleMessage traite un message reçu d'un client
func (c *Client) handleMessage(data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Erreur parsing message: %v", err)
		return
	}

	switch msg.Type {
	case "trainer_join":
		c.handleTrainerJoin(msg)

	case "stagiaire_join":
		c.handleStagiaireJoin(msg)

	case "start_vote":
		c.handleStartVote(msg)

	case "vote":
		c.handleVote(msg)

	case "close_vote":
		c.handleCloseVote(msg)

	case "reset_vote":
		c.handleResetVote(msg)

	case "update_name":
		c.handleUpdateName(msg)

	default:
		log.Printf("Type de message inconnu: %s", msg.Type)
	}
}

// handleTrainerJoin gère la connexion d'un formateur
func (c *Client) handleTrainerJoin(msg Message) {
	c.Type = "trainer"
	c.SessionID = msg.SessionCode

	c.Hub.Register <- c

	// Répondre avec la session créée
	sendJSON(c, map[string]interface{}{
		"type":        "session_created",
		"sessionId":   msg.SessionCode,
		"sessionCode": msg.SessionCode,
	})
}

// handleStagiaireJoin gère la connexion d'un stagiaire
func (c *Client) handleStagiaireJoin(msg Message) {
	c.Type = "stagiaire"
	c.ID = msg.StagiaireID
	c.Name = msg.Name
	c.SessionID = msg.SessionCode

	session := c.Hub.GetSession(msg.SessionCode)
	if session == nil || session.Trainer == nil {
		sendJSON(c, map[string]interface{}{
			"type": "join_error",
		})
		return
	}

	// Stocker le nom dans la session (persiste après déconnexion)
	if msg.Name != "" {
		c.Hub.mu.Lock()
		session.StagiaireNames[c.ID] = msg.Name
		c.Hub.mu.Unlock()
	}

	c.Hub.Register <- c

	// Répondre avec succès
	sendJSON(c, map[string]interface{}{
		"type":        "session_joined",
		"sessionId":   msg.SessionCode,
		"sessionCode": msg.SessionCode,
	})
}

// handleStartVote gère le démarrage d'un vote
func (c *Client) handleStartVote(msg Message) {
	session := c.Hub.GetSession(c.SessionID)
	if session == nil || session.Trainer != c {
		return
	}

	c.Hub.mu.Lock()
	session.VoteState = "active"
	session.ActiveColors = msg.Colors
	session.MultipleChoice = msg.MultipleChoice
	// Vider les votes précédents
	session.Votes = make(map[string][]string)
	c.Hub.mu.Unlock()

	// Broadcast à tous les stagiaires
	data, err := json.Marshal(map[string]interface{}{
		"type":           "vote_started",
		"colors":         msg.Colors,
		"multipleChoice": msg.MultipleChoice,
	})
	if err != nil {
		log.Printf("Erreur marshaling vote_started: %v", err)
		return
	}

	c.Hub.Broadcast <- &BroadcastMessage{
		SessionID: c.SessionID,
		Message:   data,
	}
}

// handleVote gère un vote d'un stagiaire
func (c *Client) handleVote(msg Message) {
	session := c.Hub.GetSession(c.SessionID)
	if session == nil {
		return
	}

	c.Hub.mu.Lock()
	// Enregistrer ou mettre à jour le vote
	session.Votes[msg.StagiaireID] = msg.Couleurs
	// Récupérer le nom du stagiaire
	stagiaireName := session.StagiaireNames[msg.StagiaireID]
	if stagiaireName == "" {
		stagiaireName = c.Name
	}
	c.Hub.mu.Unlock()

	// Envoyer la confirmation au stagiaire
	sendJSON(c, map[string]interface{}{
		"type": "vote_accepted",
	})

	// Notifier le formateur avec le nom du stagiaire
	trainerData, err := json.Marshal(map[string]interface{}{
		"type":        "vote_received",
		"stagiaireId": msg.StagiaireID,
		"stagiaireName": stagiaireName,
		"couleurs":    msg.Couleurs,
	})
	if err != nil {
		log.Printf("Erreur marshaling vote_received: %v", err)
		return
	}

	if session.Trainer != nil {
		select {
		case session.Trainer.Send <- trainerData:
		default:
		}
	}
}

// handleCloseVote gère la fermeture d'un vote
func (c *Client) handleCloseVote(_ Message) {
	session := c.Hub.GetSession(c.SessionID)
	if session == nil || session.Trainer != c {
		return
	}

	c.Hub.mu.Lock()
	session.VoteState = "closed"
	c.Hub.mu.Unlock()

	// Broadcast à tous les stagiaires
	data, err := json.Marshal(map[string]interface{}{
		"type": "vote_closed",
	})
	if err != nil {
		log.Printf("Erreur marshaling vote_closed: %v", err)
		return
	}

	c.Hub.Broadcast <- &BroadcastMessage{
		SessionID: c.SessionID,
		Message:   data,
	}
}

// handleResetVote gère la réinitialisation pour un nouveau vote
func (c *Client) handleResetVote(msg Message) {
	session := c.Hub.GetSession(c.SessionID)
	if session == nil || session.Trainer != c {
		return
	}

	c.Hub.mu.Lock()
	session.VoteState = "idle"
	session.ActiveColors = msg.Colors
	session.MultipleChoice = msg.MultipleChoice
	session.Votes = make(map[string][]string)
	c.Hub.mu.Unlock()

	// Broadcast à tous les stagiaires
	data, err := json.Marshal(map[string]interface{}{
		"type": "vote_reset",
	})
	if err != nil {
		log.Printf("Erreur marshaling vote_reset: %v", err)
		return
	}

	c.Hub.Broadcast <- &BroadcastMessage{
		SessionID: c.SessionID,
		Message:   data,
	}
}

// generateID génère un ID aléatoire sécurisé
func generateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range b {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			// Fallback vers un ID basé sur le temps si crypto/rand échoue
			log.Printf("Erreur génération ID aléatoire: %v", err)
			return fallbackGenerateID()
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// fallbackGenerateID génère un ID de secours basé sur le timestamp
func fallbackGenerateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	// Utiliser nanosecondes comme source d'entropie
	nano := time.Now().UnixNano()
	b := make([]byte, 12)
	for i := range b {
		b[i] = charset[(i+int(nano))%len(charset)]
		nano = nano >> 4 // Shift pour varier l'index
	}
	return string(b)
}

// handleUpdateName gère la mise à jour du nom d'un stagiaire
func (c *Client) handleUpdateName(msg Message) {
	if msg.Name == "" {
		return
	}

	c.Hub.UpdateStagiaireName(c.SessionID, c.ID, msg.Name)

	// Mettre à jour le nom du client actuel aussi
	c.Name = msg.Name

	// Confirmation au stagiaire
	sendJSON(c, map[string]interface{}{
		"type": "name_updated",
		"name": msg.Name,
	})
}
