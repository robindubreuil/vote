package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/server"
)

// TestRunHealthCheckGreen is the D10 contract: `vote-server --health`
// (the distroless HEALTHCHECK entrypoint, which ships no wget/curl) does
// an HTTP GET to /livez on loopback and returns 0 when the server
// answers 200. We stand up a real httptest server on an ephemeral port,
// point PORT at it, and assert the exit code. The probe targets
// 127.0.0.1 so it works regardless of HOST binding, which we verify by
// leaving HOST unset.
func TestRunHealthCheckGreen(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/livez" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	port := portFromURL(t, ts.URL)
	withEnvPort(t, port)

	if got := runHealthCheck(); got != 0 {
		t.Fatalf("runHealthCheck against live server: want exit 0, got %d", got)
	}
}

// TestRunHealthCheckNon200 is the "server up but unhealthy" branch: a
// non-200 from /livez must surface as exit 1 so the orchestrator treats
// the container as unhealthy.
func TestRunHealthCheckNon200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	withEnvPort(t, portFromURL(t, ts.URL))

	if got := runHealthCheck(); got != 1 {
		t.Fatalf("runHealthCheck against 503 server: want exit 1, got %d", got)
	}
}

// TestRunHealthCheckDialFailure covers the "nothing listening" branch:
// connection refused → exit 1. We grab the port from a server we then
// close, so the port is almost certainly free and the dial fails fast.
func TestRunHealthCheckDialFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := ts.URL
	ts.Close()

	withEnvPort(t, portFromURL(t, addr))

	if got := runHealthCheck(); got != 1 {
		t.Fatalf("runHealthCheck with no server: want exit 1, got %d", got)
	}
}

// TestRunHealthCheckHonoursPortEnv pins that the probe honours the same
// PORT env var the server listens on — if it hard-coded 8080, a server
// running on a custom port would be reported dead.
func TestRunHealthCheckHonoursPortEnv(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	customPort := portFromURL(t, ts.URL)
	withEnvPort(t, customPort)

	// If runHealthCheck ignored PORT and dialled :8080, it would get a
	// connection-refused (nothing on 8080 in CI) and return 1.
	if got := runHealthCheck(); got != 0 {
		t.Fatalf("runHealthCheck should honour PORT=%s, want exit 0, got %d", customPort, got)
	}
}

func portFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Port()
}

// withEnvPort sets PORT for the duration of the test and restores the
// previous value on cleanup. t.Setenv would be ideal but is only
// available in Go 1.17+; t.Setenv exists, so prefer it for the
// automatic restore and t.Parallel safety.
func withEnvPort(t *testing.T, port string) {
	t.Helper()
	t.Setenv("PORT", port)
}

// trackingListener wraps a net.Listener to record the moment Close() is
// called, so TestShutdownClosesListenerFirst can assert that the HTTP
// listener closes BEFORE the hub context cancels.
type trackingListener struct {
	net.Listener
	onClose func()
}

func (t *trackingListener) Close() error {
	if t.onClose != nil {
		t.onClose()
	}
	return t.Listener.Close()
}

// TestShutdownClosesListenerFirst is the R2 ordering contract.
//
// gracefulShutdown must close the HTTP listener (srv.Shutdown) BEFORE
// cancelling the hub context (h.Shutdown). Reversing the order reopens
// the race the rest of R2 closes: a dial accepted during drain would
// call client.Start() → wg.Add(2) concurrent with the Hub's wg.Wait,
// which Go forbids when the counter is zero ("sync: WaitGroup is
// reused before previous Wait has returned").
//
// The test records an ordered event log: the listener's onClose fires
// synchronously inside srv.Shutdown (which runs to completion inside
// gracefulShutdown before h.Shutdown starts), and a background
// goroutine records when the hub context cancels. Asserting the event
// order is [listener, hub] pins the structural contract independent of
// wall-clock resolution.
func TestShutdownClosesListenerFirst(t *testing.T) {
	cfg := &config.Config{
		AllowedOrigins:  []string{"*"},
		PingInterval:    time.Hour,
		CleanupInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	h.Run()

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var (
		orderMu sync.Mutex
		order   []string
	)
	record := func(name string) {
		orderMu.Lock()
		order = append(order, name)
		orderMu.Unlock()
	}

	tl := &trackingListener{Listener: raw, onClose: func() { record("listener") }}

	srv := server.NewServer(cfg, h)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(tl) }()

	// Wait for readiness so the readiness probe doesn't influence timing.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + raw.Addr().String() + "/livez")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Observer for the hub context cancellation. Channels synchronize
	// the read so the test doesn't sample order before the observer
	// goroutine has had a chance to run.
	hubCancelled := make(chan struct{})
	go func() {
		<-h.Context().Done()
		record("hub")
		close(hubCancelled)
	}()

	gracefulShutdown(srv, h, 5*time.Second)
	<-hubCancelled

	if err := <-serveErr; err != nil && err != http.ErrServerClosed {
		t.Logf("serve returned non-ErrServerClosed: %v", err)
	}

	orderMu.Lock()
	defer orderMu.Unlock()
	if len(order) != 2 || order[0] != "listener" || order[1] != "hub" {
		t.Errorf("expected event order [listener, hub], got %v", order)
	}
}
