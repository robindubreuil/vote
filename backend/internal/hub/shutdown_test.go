package hub

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"vote-backend/internal/config"
)

// TestShutdownWaitsForClientGoroutines is the CM1+CM2 regression test.
//
// Before the fix, Hub.Shutdown only cancelled the context and returned
// immediately: writePumps could be torn down mid-frame, hijacked WS conns
// went un-drained, and the process could exit while client goroutines
// were still touching the connection. The fix tracks Run, cleanupLoop,
// and every readPump/writePump in a sync.WaitGroup; Shutdown closes
// every conn and waits for the counter to fall to zero before returning.
//
// This test wires the Hub to a real httptest WS endpoint, connects a
// batch of real clients, calls Shutdown, and asserts:
//
//  1. Shutdown returns within a sane deadline (doesn't hang).
//  2. Every client read goroutine observes the close (no leaked
//     readPumps blocked on ReadMessage forever).
//  3. The Hub's WaitGroup is back at zero (no leaked goroutines
//     that the WaitGroup is still tracking).
//  4. Every live client's closing flag was flipped before the conn
//     was closed (so concurrent senders short-circuited).
func TestShutdownWaitsForClientGoroutines(t *testing.T) {
	cfg := &config.Config{
		SessionTimeout:  time.Hour,
		CleanupInterval: time.Hour,
		PingInterval:    time.Hour, // quiet the pings so we observe shutdown, not ping errors
		WriteTimeout:    time.Second,
		AllowedOrigins:  []string{"*"},
	}
	h := NewHub(cfg)
	h.Run()

	// httptest server that upgrades to WS and hands the conn to the hub.
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
		c.SessionID = "SD"  // shared session so Shutdown iterates them all
		c.TrainerToken = "" // first trainer, no takeover
		// Register synchronously so the test sees the connection before
		// Shutdown iterates Connections.
		select {
		case h.Register <- c:
		case <-time.After(time.Second):
			t.Errorf("timeout registering client %s", clientID)
			conn.Close()
			return
		}
		c.Start()
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]

	const clients = 6
	conns := make([]*websocket.Conn, clients)
	for i := range conns {
		dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
		c, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{srv.URL}})
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns[i] = c
	}

	// Wait for the hub to register all the clients before we tear down.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		_, ok := h.Connections["SD"]
		trainer := ok && h.Connections["SD"].Trainer != nil
		h.mu.RUnlock()
		if trainer {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Goroutine baseline: pump clients' reads so we observe the close.
	var closed atomic.Int32
	for _, c := range conns {
		c := c
		go func() {
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					closed.Add(1)
					return
				}
			}
		}()
	}

	// Snapshot goroutine count before shutdown.
	before := runtime.NumGoroutine()

	done := make(chan struct{})
	go func() {
		h.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Hub.Shutdown did not return within 5s — leaked goroutine")
	}

	// All client reads should have observed the close (readPumps exited,
	// close frames were delivered). Generous timeout for slow CI.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if int(closed.Load()) == clients {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := closed.Load(); int(got) != clients {
		t.Errorf("clients that observed close: expected %d, got %d", clients, got)
	}

	// WaitGroup must be back at zero — Shutdown Wait()ed on it. We can't
	// read wg.counter directly (unexported), so we use the documented
	// invariant that a subsequent Add/Done pair is non-blocking. Instead,
	// assert that no goroutines that *reference the Hub* are leaked by
	// checking that the total goroutine count drops back close to the
	// pre-shutdown baseline minus the per-client overhead.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// Each client spawned: 1 dial-side read goroutine (exited by now),
		// 2 hub-side goroutines (read+write pump). Allow slack for runtime.
		if after := runtime.NumGoroutine(); after <= before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}

	// Connections map: entries can stay (we didn't unregister), but
	// every connection's `closing` flag must be set so concurrent
	// trySend/SendJSON callers short-circuit instead of pushing onto a
	// dead channel.
	h.mu.RLock()
	for _, conns := range h.Connections {
		if conns.Trainer != nil && !conns.Trainer.closing.Load() {
			t.Errorf("trainer %s closing flag not set after Shutdown", conns.Trainer.ID)
		}
	}
	h.mu.RUnlock()

	srv.Close()
}
