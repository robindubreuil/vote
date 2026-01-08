package main

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Client représente une connexion WebSocket
type Client struct {
	ID        string
	Name      string // Prénom du stagiaire (vide pour le formateur)
	SessionID string
	Type      string // "trainer" ou "stagiaire"
	Conn      *websocket.Conn
	Send      chan []byte
	Hub       *Hub
}

// Hub gère toutes les connexions WebSocket et le broadcast
type Hub struct {
	// Clients connectés par session
	Sessions map[string]*SessionState

	// Register les nouveaux clients
	Register chan *Client

	// Unregister les clients déconnectés
	Unregister chan *Client

	// Messages broadcast à tous les clients d'une session
	Broadcast chan *BroadcastMessage

	mu sync.RWMutex
}

// BroadcastMessage représente un message à broadcast
type BroadcastMessage struct {
	SessionID string
	Message   []byte
	ExcludeID string // Optionnel: ne pas envoyer à ce client
}

// SessionState représente l'état d'une session de vote
type SessionState struct {
	SessionCode   string
	Trainer       *Client
	Stagiaires    map[string]*Client
	StagiaireNames map[string]string // stagiaireID -> prénom (persiste après déconnexion)
	VoteState     string             // "idle", "active", "closed"
	ActiveColors  []string
	MultipleChoice bool
	Votes         map[string][]string // stagiaireID -> couleurs
}

// NewHub crée un nouveau Hub
func NewHub() *Hub {
	return &Hub{
		Sessions:  make(map[string]*SessionState),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Broadcast:  make(chan *BroadcastMessage),
	}
}

// Run lance la boucle principale du Hub
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.registerClient(client)

		case client := <-h.Unregister:
			h.unregisterClient(client)

		case broadcast := <-h.Broadcast:
			h.broadcastToSession(broadcast)
		}
	}
}

func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.Sessions[client.SessionID]
	if !exists {
		if client.Type == "trainer" {
			// Créer une nouvelle session
			session = &SessionState{
				SessionCode:    client.SessionID,
				Trainer:        client,
				Stagiaires:     make(map[string]*Client),
				StagiaireNames: make(map[string]string),
				VoteState:      "idle",
				Votes:          make(map[string][]string),
			}
			h.Sessions[client.SessionID] = session
			log.Printf("Nouvelle session créée: %s", client.SessionID)
		} else {
			// Stagiaire essaye de rejoindre une session inexistante
			sendError(client, "Session non trouvée")
			return
		}
	}

	if client.Type == "trainer" {
		session.Trainer = client
		log.Printf("Formateur connecté à la session: %s", client.SessionID)
	} else {
		// Vérifier qu'il y a un formateur
		if session.Trainer == nil {
			sendError(client, "Session non disponible (pas de formateur)")
			return
		}

		// Vérifier si le stagiaire est déjà connecté
		if existingClient, exists := session.Stagiaires[client.ID]; exists {
			// Remplacer l'ancienne connexion proprement
			// Fermer le channel et la connexion pour stopper les goroutines immédiatement
			close(existingClient.Send)
			if existingClient.Conn != nil {
				existingClient.Conn.Close()
			}
			delete(session.Stagiaires, client.ID)
		}

		session.Stagiaires[client.ID] = client
		log.Printf("Stagiaire %s connecté à la session: %s", client.ID, client.SessionID)

		// Notifier le formateur
		h.notifyTrainerSessionUpdate(session)

		// Envoyer la configuration actuelle au stagiaire
		if session.VoteState == "active" {
			sendJSON(client, map[string]interface{}{
				"type":           "vote_started",
				"colors":         session.ActiveColors,
				"multipleChoice": session.MultipleChoice,
			})
		}
	}
}

func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.Sessions[client.SessionID]
	if !exists {
		return
	}

	if client.Type == "trainer" {
		// Formateur déconnecté - fermer toute la session
		log.Printf("Formateur déconnecté, session %s fermée", client.SessionID)
		delete(h.Sessions, client.SessionID)

		// Déconnecter tous les stagiaires
		for _, stagiaire := range session.Stagiaires {
			if stagiaire.Conn != nil {
				stagiaire.Conn.Close()
			}
		}
	} else {
		// Stagiaire déconnecté
		// Vérifier que c'est bien le même objet client (pas une reconnexion)
		if currentClient, exists := session.Stagiaires[client.ID]; exists && currentClient == client {
			delete(session.Stagiaires, client.ID)
			log.Printf("Stagiaire %s déconnecté de la session: %s", client.ID, client.SessionID)

			// Notifier le formateur
			h.notifyTrainerSessionUpdate(session)
		}
	}
}

func (h *Hub) broadcastToSession(broadcast *BroadcastMessage) {
	h.mu.RLock()
	session, exists := h.Sessions[broadcast.SessionID]
	if !exists {
		h.mu.RUnlock()
		return
	}

	// Prendre une snapshot des destinataires sous le lock pour éviter la race condition
	var trainer *Client
	if session.Trainer != nil && session.Trainer.ID != broadcast.ExcludeID {
		trainer = session.Trainer
	}

	// Copier la map des stagiaires pour éviter itérer pendant modification
	stagiaires := make([]*Client, 0, len(session.Stagiaires))
	for _, client := range session.Stagiaires {
		if client.ID != broadcast.ExcludeID {
			stagiaires = append(stagiaires, client)
		}
	}
	h.mu.RUnlock()

	// Envoyer au formateur (hors du lock)
	if trainer != nil {
		select {
		case trainer.Send <- broadcast.Message:
		default:
			close(trainer.Send)
		}
	}

	// Envoyer aux stagiaires (hors du lock)
	for _, client := range stagiaires {
		select {
		case client.Send <- broadcast.Message:
		default:
			close(client.Send)
		}
	}
}

// notifyTrainerSessionUpdate notifie le formateur des changements de connexion
func (h *Hub) notifyTrainerSessionUpdate(session *SessionState) {
	if session.Trainer == nil {
		return
	}

	// Construire la liste des stagiaires connectés avec leurs noms
	stagiaires := make([]map[string]interface{}, 0, len(session.Stagiaires))
	for id, client := range session.Stagiaires {
		name := client.Name
		if name == "" {
			// Fallback sur le nom stocké si pas de nom dans le client
			name = session.StagiaireNames[id]
		}
		stagiaires = append(stagiaires, map[string]interface{}{
			"id":   id,
			"name": name,
		})
	}

	sendJSON(session.Trainer, map[string]interface{}{
		"type":        "connected_count",
		"count":       len(session.Stagiaires),
		"stagiaires":  stagiaires,
	})
}

// UpdateStagiaireName met à jour le nom d'un stagiaire et notifie le formateur
func (h *Hub) UpdateStagiaireName(sessionID, stagiaireID, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.Sessions[sessionID]
	if !exists {
		return
	}

	// Stocker le nom (persiste même après déconnexion)
	session.StagiaireNames[stagiaireID] = name

	// Mettre à jour le nom du client connecté si présent
	if client, exists := session.Stagiaires[stagiaireID]; exists {
		client.Name = name
	}

	// Récupérer ou construire la liste des stagiaires
	stagiaires := make([]map[string]interface{}, 0, len(session.Stagiaires))
	for id, client := range session.Stagiaires {
		stagiaires = append(stagiaires, map[string]interface{}{
			"id":   id,
			"name": client.Name,
		})
	}

	// Envoyer la liste complète au formateur pour synchronisation
	if session.Trainer != nil {
		sendJSON(session.Trainer, map[string]interface{}{
			"type":        "stagiaire_names_updated",
			"stagiaires":  stagiaires,
		})
	}
}

// GetSession récupère l'état d'une session
func (h *Hub) GetSession(sessionID string) *SessionState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.Sessions[sessionID]
}

// SendJSON envoie un message JSON à un client
func sendJSON(client *Client, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case client.Send <- data:
		return nil
	default:
		return nil
	}
}

// sendError envoie un message d'erreur à un client
func sendError(client *Client, message string) {
	sendJSON(client, map[string]interface{}{
		"type":    "error",
		"message": message,
	})
}
