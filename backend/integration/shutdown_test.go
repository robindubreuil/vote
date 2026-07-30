package integration

import (
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/models"
)

// TestShutdownJoinsAllGoroutinesUnderLoad is the R2 -race integration test
// that would have caught the bug the existing shutdown_test.go missed.
//
// The existing TestShutdownWaitsForClientGoroutines (hub package)
// exercises Hub.Shutdown in isolation — it connects clients directly to
// the hub via a custom httptest handler, bypassing the real server /
// handleWebSocket path. That is why the original main.go ordering bug
// (hub.Shutdown before srv.Shutdown) stayed latent: no test loaded
// main.go's shutdown path, so the WaitGroup contract violation
// ("sync: WaitGroup is reused before previous Wait has returned" when a
// positive-delta Add races with Wait at counter zero) and the stuck-
// socket freeze (a reconnect upgraded during drain, its readPump then
// blocking in ReadMessage until pongWait) never surfaced.
//
// This test loads the REAL server (TestServer → server.Server +
// handleWebSocket, the same dial path production uses) with a batch of
// registered clients plus a reconnect storm that continuously re-dials
// throughout shutdown. It then triggers drain via TestServer.Close
// (which mirrors main.go's gracefulShutdown order: srv.Shutdown BEFORE
// h.Shutdown) and asserts:
//
//  1. No panic — the -race detector and WaitGroup's own checks would
//     fire if a client.Start() → wg.Add(2) raced with wg.Wait.
//  2. Every client read goroutine observes the close.
//  3. The process goroutine count returns to baseline (no leak).
func TestShutdownJoinsAllGoroutinesUnderLoad(t *testing.T) {
	ts := NewTestServer(t)
	wsURL := ts.WebSocketURL()
	headers := http.Header{"Origin": []string{"http://localhost"}}

	// Connect a batch of clients and register each as a trainer in its
	// own session, so hub.Connections is populated and hub.Shutdown
	// iterates them (closing their conns and waiting on their pumps).
	const registered = 12
	var (
		conns       []*websocket.Conn
		readWG      sync.WaitGroup
		closedCount atomic.Int32
	)
	readWG.Add(registered)
	for i := 0; i < registered; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns = append(conns, c)

		// Register as a trainer so the hub tracks this connection.
		if err := c.WriteJSON(models.Message{Type: "trainer_join"}); err != nil {
			t.Fatalf("trainer_join write %d: %v", i, err)
		}

		// Read session_created BEFORE handing the conn to the read
		// pump: gorilla/websocket forbids concurrent reads on one conn
		// (one reader owns it at a time), so we synchronously consume
		// the join response here, then hand off.
		var msg map[string]any
		if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		if err := c.ReadJSON(&msg); err != nil {
			t.Fatalf("read session_created: %v", err)
		}
		if msg["type"] != "session_created" {
			t.Fatalf("expected session_created, got %v", msg["type"])
		}
		_ = c.SetReadDeadline(time.Time{})

		// Read pump takes sole ownership of reads from here; it observes
		// the close frame the hub sends during Shutdown.
		go func(c *websocket.Conn) {
			defer readWG.Done()
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					closedCount.Add(1)
					return
				}
			}
		}(c)
	}

	// Reconnect storm: continuously re-dial throughout shutdown. This is
	// the load that exposes the race in the old ordering — without the
	// drain guard and listener-first order, these dials would upgrade
	// during drain and call wg.Add(2) concurrent with wg.Wait.
	var (
		stormStop atomic.Bool
		stormWG   sync.WaitGroup
	)
	stormWG.Add(4)
	for i := 0; i < 4; i++ {
		go func() {
			defer stormWG.Done()
			for !stormStop.Load() {
				c, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
				if err != nil {
					continue // listener closed mid-dial during drain — expected
				}
				// Drain until close; the drain guard / listener close ends us.
				for {
					if _, _, err := c.ReadMessage(); err != nil {
						break
					}
				}
				_ = c.Close()
			}
		}()
	}

	// Let the storm build up some concurrent reconnect pressure.
	time.Sleep(300 * time.Millisecond)
	before := runtime.NumGoroutine()

	// Trigger shutdown via the same path main.go uses. ts.Close mirrors
	// gracefulShutdown: srv.Shutdown (listener first) then h.Shutdown
	// (cancel ctx, close conns, wg.Wait). A panic here is the failure
	// signal — the WaitGroup contract violation surfaces under -race.
	ts.Close(t)

	// Every registered client's read pump must observe the close — the
	// hub's Conn.Close in Shutdown unblocks ReadMessage.
	readWG.Wait()
	if got := closedCount.Load(); int(got) != registered {
		t.Errorf("registered clients closed: want %d, got %d", registered, got)
	}

	// Stop the storm and let its goroutines observe the closed listener
	// (their dials fail-fast) so they exit.
	stormStop.Store(true)
	stormWG.Wait()

	// Goroutine count must settle back to (or below) baseline. Generous
	// deadline for slow CI schedulers.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Errorf("goroutine leak: before=%d after=%d", before, got)
	}
}
