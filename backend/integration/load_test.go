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

// LoadClient is a simplified WS client that returns errors instead of calling t.Fatal.
type LoadClient struct {
	conn      *websocket.Conn
	send      chan []byte
	recv      chan []byte
	closeOnce sync.Once
	closed    chan struct{}
	id        string
}

func NewLoadClient(wsURL string) (*LoadClient, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	client := &LoadClient{
		conn:   conn,
		send:   make(chan []byte, 100),
		recv:   make(chan []byte, 100),
		closed: make(chan struct{}),
	}

	go client.readPump()
	go client.writePump()

	return client, nil
}

func (c *LoadClient) readPump() {
	defer c.Close()
	c.conn.SetReadDeadline(time.Now().Add(30 * time.Second)) // Longer deadline for load
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		select {
		case c.recv <- message:
		case <-c.closed:
			return
		}
	}
}

func (c *LoadClient) writePump() {
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

func (c *LoadClient) SendMessage(msg models.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case c.send <- data:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("timeout sending message")
	}
}

func (c *LoadClient) WaitForType(msgType string, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case data := <-c.recv:
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				return nil, err
			}
			if t, ok := msg["type"].(string); ok && t == msgType {
				return msg, nil
			}
		case <-time.After(100 * time.Millisecond):
			continue
		}
	}
	return nil, fmt.Errorf("timeout waiting for %s", msgType)
}

func (c *LoadClient) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.conn.Close()
		close(c.send)
	})
}

func TestLoadMultipleSessions(t *testing.T) {
	// Setup server
	ts := NewTestServer(t)
	defer ts.Close(t)

	// Parameters
	numSessions := 10
	numStagiairesPerSession := 20
	wsURL := ts.WebSocketURL()

	var wg sync.WaitGroup
	errChan := make(chan error, numSessions*(numStagiairesPerSession+1))

	// Launch sessions
	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(sessionIdx int) {
			defer wg.Done()
			if err := runSessionScenario(sessionIdx, numStagiairesPerSession, wsURL); err != nil {
				errChan <- fmt.Errorf("Session %d error: %w", sessionIdx, err)
			}
		}(i)
	}

	// Wait for all sessions to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Errorf("Encountered %d errors during load test:", len(errs))
		for _, err := range errs {
			t.Log(err)
		}
		t.FailNow()
	}
}

func runSessionScenario(sessionIdx, numStagiaires int, wsURL string) error {
	// 1. Trainer joins
	trainer, err := NewLoadClient(wsURL)
	if err != nil {
		return err
	}
	defer trainer.Close()

	if err := trainer.SendMessage(TrainerJoin("").Build()); err != nil {
		return err
	}

	msg, err := trainer.WaitForType("session_created", 5*time.Second)
	if err != nil {
		return err
	}
	sessionCode, ok := msg["sessionCode"].(string)
	if !ok {
		return errors.New("invalid session code")
	}

	// 2. Stagiaires join
	var stagiaires []*LoadClient
	var sWg sync.WaitGroup
	clientsChan := make(chan *LoadClient, numStagiaires)
	sErrChan := make(chan error, numStagiaires)

	for i := 0; i < numStagiaires; i++ {
		sWg.Add(1)
		go func(idx int) {
			defer sWg.Done()
			s, err := NewLoadClient(wsURL)
			if err != nil {
				sErrChan <- err
				return
			}

			// Use unique IDs for each stagiaire - let server generate most of it
			id := fmt.Sprintf("s%08d_%d", sessionIdx, idx)
			if err := s.SendMessage(StagiaireJoin(sessionCode, id, fmt.Sprintf("User%d", idx)).Build()); err != nil {
				s.Close()
				sErrChan <- err
				return
			}

			if _, err := s.WaitForType("session_joined", 10*time.Second); err != nil {
				s.Close()
				sErrChan <- err
				return
			}
			clientsChan <- s
		}(i)
	}

	sWg.Wait()
	close(clientsChan)
	close(sErrChan)

	// Collect successful clients
	for s := range clientsChan {
		stagiaires = append(stagiaires, s)
	}

	// Check for errors
	for err := range sErrChan {
		return fmt.Errorf("stagiaire join failed: %w", err)
	}

	defer func() {
		for _, s := range stagiaires {
			s.Close()
		}
	}()

	if len(stagiaires) != numStagiaires {
		return fmt.Errorf("expected %d stagiaires, got %d", numStagiaires, len(stagiaires))
	}

	// Wait for trainer to see all connections
	// Depending on timing, trainer might see intermediate counts.
	// We just want to ensure it eventually sees everyone.
	// We'll give it a few seconds.
	timeout := time.After(10 * time.Second)
	seenCount := 0
	for {
		msg, err := trainer.WaitForType("connected_count", 2*time.Second)
		if err != nil {
			// If we time out waiting for an update, check if we already reached it?
			// Actually WaitForType returns error on timeout.
			select {
			case <-timeout:
				return fmt.Errorf("timeout waiting for connected_count %d, last seen %d", numStagiaires, seenCount)
			default:
				// Retry wait
				continue
			}
		}

		if count, ok := msg["count"].(float64); ok {
			seenCount = int(count)
			if seenCount == numStagiaires {
				break
			}
		}

		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for connected_count %d, last seen %d", numStagiaires, seenCount)
		default:
		}
	}

	// 3. Start Vote
	colors := []string{"rouge", "bleu", "vert"}
	if err := trainer.SendMessage(StartVote(colors, false).Build()); err != nil {
		return err
	}

	if _, err := trainer.WaitForType("vote_started", 5*time.Second); err != nil {
		return err
	}

	// 4. Stagiaires Vote
	voteWg := sync.WaitGroup{}
	voteErrChan := make(chan error, numStagiaires)

	for i, s := range stagiaires {
		voteWg.Add(1)
		go func(client *LoadClient, idx int) {
			defer voteWg.Done()
			// Wait for vote started
			if _, err := client.WaitForType("vote_started", 5*time.Second); err != nil {
				voteErrChan <- err
				return
			}

			// Vote
			color := colors[idx%len(colors)]
			if err := client.SendMessage(Vote(color).Build()); err != nil {
				voteErrChan <- err
				return
			}

			if _, err := client.WaitForType("vote_accepted", 5*time.Second); err != nil {
				voteErrChan <- err
				return
			}
		}(s, i)
	}

	voteWg.Wait()
	close(voteErrChan)

	for err := range voteErrChan {
		return fmt.Errorf("voting failed: %w", err)
	}

	// 5. Trainer receives votes
	// Trainer should receive 'vote_received' for each stagiaire
	receivedVotes := 0
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) && receivedVotes < numStagiaires {
		if _, err := trainer.WaitForType("vote_received", 1*time.Second); err == nil {
			receivedVotes++
		}
	}

	if receivedVotes != numStagiaires {
		return fmt.Errorf("trainer expected %d votes, got %d", numStagiaires, receivedVotes)
	}

	return nil
}
