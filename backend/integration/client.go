package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/models"
)

// WSClient wraps a gorilla/websocket connection for testing.
// It provides helpers for sending/receiving messages and synchronization.
type WSClient struct {
	conn      *websocket.Conn
	send      chan []byte
	recv      chan []byte
	closeOnce sync.Once
	closed    chan struct{}
	t         *testing.T
	id        string
	sessionID string
}

// NewWSClient creates a new WebSocket client connected to the test server.
// The wsURL parameter should be the WebSocket URL to connect to.
func NewWSClient(t *testing.T, wsURL string) *WSClient {
	t.Helper()

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}

	client := &WSClient{
		conn:   conn,
		send:   make(chan []byte, 100),
		recv:   make(chan []byte, 100),
		closed: make(chan struct{}),
		t:      t,
	}

	// Start read pump
	go client.readPump()

	// Start write pump
	go client.writePump()

	return client
}

// readPump reads messages from the connection and pushes to recv channel.
func (c *WSClient) readPump() {
	defer c.Close()

	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.t.Logf("WebSocket read error: %v", err)
			}
			return
		}
		select {
		case c.recv <- message:
		case <-c.closed:
			return
		}
	}
}

// writePump writes messages from send channel to the connection.
func (c *WSClient) writePump() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer c.Close()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

// SendMessage sends a message to the server.
func (c *WSClient) SendMessage(msg models.Message) {
	c.t.Helper()

	data, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("Failed to marshal message: %v", err)
	}

	select {
	case c.send <- data:
	case <-time.After(5 * time.Second):
		c.t.Fatal("Timeout sending message")
	}
}

// SendMessageAsync sends a message without blocking.
func (c *WSClient) SendMessageAsync(msg models.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	select {
	case c.send <- data:
		return nil
	case <-time.After(100 * time.Millisecond):
		return errors.New("send channel full")
	}
}

// ReceiveMessage waits for and returns the next message.
func (c *WSClient) ReceiveMessage(timeout time.Duration) map[string]interface{} {
	c.t.Helper()

	select {
	case data := <-c.recv:
		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			c.t.Fatalf("Failed to unmarshal message: %v", err)
		}
		return msg
	case <-time.After(timeout):
		c.t.Fatal("Timeout waiting for message")
		return nil
	}
}

// ReceiveMessageTyped waits for and returns the next message as a map.
func (c *WSClient) ReceiveMessageTyped(timeout time.Duration) map[string]any {
	return c.ReceiveMessage(timeout)
}

// TryReceiveMessage attempts to receive a message without blocking forever.
func (c *WSClient) TryReceiveMessage(timeout time.Duration) (map[string]interface{}, bool) {
	c.t.Helper()

	select {
	case data := <-c.recv:
		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			c.t.Logf("Failed to unmarshal message: %v", err)
			return nil, false
		}
		return msg, true
	case <-time.After(timeout):
		return nil, false
	}
}

// WaitForType waits for a message of the specified type.
// It drains non-matching messages silently (they're expected in the protocol).
func (c *WSClient) WaitForType(msgType string, timeout time.Duration) map[string]interface{} {
	c.t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 10 * time.Millisecond
		}
		msg, ok := c.TryReceiveMessage(remaining)
		if !ok {
			continue
		}
		if t, ok := msg["type"].(string); ok && t == msgType {
			return msg
		}
		// Not the type we want - drain and continue
		// Common messages we might see: config_updated, connected_count, etc.
	}
	c.t.Fatalf("Timeout waiting for message type: %s", msgType)
	return nil
}

// DrainMessages reads and discards all pending messages.
func (c *WSClient) DrainMessages() {
	for {
		select {
		case <-c.recv:
		default:
			return
		}
	}
}

// Close closes the WebSocket connection.
func (c *WSClient) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.conn.Close()
		close(c.send)
	})
}

// ID returns the client ID (populated after joining).
func (c *WSClient) ID() string {
	return c.id
}

// SetID sets the client ID.
func (c *WSClient) SetID(id string) {
	c.id = id
}

// SessionID returns the session ID (populated after joining).
func (c *WSClient) SessionID() string {
	return c.sessionID
}

// SetSessionID sets the session ID.
func (c *WSClient) SetSessionID(sessionID string) {
	c.sessionID = sessionID
}

// MessageBuilder helps construct test messages.
type MessageBuilder struct {
	msg models.Message
}

// NewMessage creates a new message builder.
func NewMessage(msgType string) *MessageBuilder {
	return &MessageBuilder{
		msg: models.Message{Type: msgType},
	}
}

// SessionCode sets the session code.
func (b *MessageBuilder) SessionCode(code string) *MessageBuilder {
	b.msg.SessionCode = code
	return b
}

// StagiaireID sets the stagiaire ID.
func (b *MessageBuilder) StagiaireID(id string) *MessageBuilder {
	b.msg.StagiaireID = id
	return b
}

// Name sets the name.
func (b *MessageBuilder) Name(name string) *MessageBuilder {
	b.msg.Name = name
	return b
}

// Colors sets the colors.
func (b *MessageBuilder) Colors(colors ...string) *MessageBuilder {
	b.msg.Colors = colors
	return b
}

// MultipleChoice sets multiple choice flag.
func (b *MessageBuilder) MultipleChoice(mc bool) *MessageBuilder {
	b.msg.MultipleChoice = mc
	return b
}

// Labels sets the color labels.
func (b *MessageBuilder) Labels(labels map[string]string) *MessageBuilder {
	b.msg.Labels = labels
	return b
}

// Build returns the constructed message.
func (b *MessageBuilder) Build() models.Message {
	return b.msg
}

// Helper shortcuts for common messages

// TrainerJoin creates a trainer_join message.
func TrainerJoin(code string) *MessageBuilder {
	return NewMessage("trainer_join").SessionCode(code)
}

// StagiaireJoin creates a stagiaire_join message.
func StagiaireJoin(code, id, name string) *MessageBuilder {
	return NewMessage("stagiaire_join").SessionCode(code).StagiaireID(id).Name(name)
}

// StartVote creates a start_vote message.
func StartVote(colors []string, multipleChoice bool) *MessageBuilder {
	return NewMessage("start_vote").Colors(colors...).MultipleChoice(multipleChoice)
}

// Vote creates a vote message.
func Vote(colors ...string) *MessageBuilder {
	return NewMessage("vote").Colors(colors...)
}

// CloseVote creates a close_vote message.
func NewCloseVote() *MessageBuilder {
	return NewMessage("close_vote")
}

// ResetVote creates a reset_vote message.
func ResetVote(colors []string, multipleChoice bool) *MessageBuilder {
	return NewMessage("reset_vote").Colors(colors...).MultipleChoice(multipleChoice)
}

// UpdateName creates an update_name message.
func UpdateName(name string) *MessageBuilder {
	return NewMessage("update_name").Name(name)
}
