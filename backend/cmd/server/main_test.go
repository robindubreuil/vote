package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
