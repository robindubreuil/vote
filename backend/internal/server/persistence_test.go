package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

func newTestServerWithData(t *testing.T, secret string) *Server {
	t.Helper()
	cfg := &config.Config{
		Port:                "8080",
		DashboardSecret:     secret,
		DashboardMaxAge:     time.Hour,
		DataDir:             t.TempDir(),
		StatsSampleInterval: 100 * time.Millisecond,
	}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)
	if err := srv.EnablePersistence(); err != nil {
		t.Fatalf("EnablePersistence: %v", err)
	}
	t.Cleanup(func() { srv.CloseStore() })
	return srv
}

// loginCookie mints a valid auth cookie and returns it for use in requests.
func loginCookie(t *testing.T, srv *Server, secret string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/login", strings.NewReader("password="+secret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "vote_admin" {
			return c
		}
	}
	t.Fatal("no auth cookie minted")
	return nil
}

func TestHistoryRequiresAuth(t *testing.T) {
	srv := newTestServerWithData(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard/history", nil)
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("unauthed history: expected 302, got %d", w.Code)
	}
}

func TestHistoryReturnsPersistedSamples(t *testing.T) {
	srv := newTestServerWithData(t, "s3cr3t")
	mgr := srv.hub.VoteManager
	mgr.CreateSession("ABC", "t1")
	mgr.JoinStagiaire("ABC", "stagiaire001", "Alice")
	mgr.StartVote("ABC", "t1", []string{"rouge"}, false, nil, false)
	mgr.SubmitVote("ABC", "stagiaire001", []string{"rouge"})

	// Sampling goroutine ticks every 100ms; also force a synchronous flush.
	time.Sleep(250 * time.Millisecond)
	srv.FlushStats()

	cookie := loginCookie(t, srv, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard/history", nil)
	req.AddCookie(cookie)
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var samples []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &samples); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
	}
	if len(samples) == 0 {
		t.Fatalf("expected at least one sample, got 0")
	}
	// The oldest sample is the startup zero-flush; the most recent reflects
	// the activity driven above.
	last := samples[len(samples)-1]
	if last["sc"].(float64) < 1 {
		t.Errorf("last sample sc should reflect the session created, got %v", last["sc"])
	}
	if last["vc"].(float64) < 1 {
		t.Errorf("last sample vc should reflect the vote cast, got %v", last["vc"])
	}
}

func TestHistoryLimitInvalidQueryIgnored(t *testing.T) {
	srv := newTestServerWithData(t, "s3cr3t")
	cookie := loginCookie(t, srv, "s3cr3t")
	for _, q := range []string{"?limit=abc", "?limit=-5", "?limit=999999999"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/dashboard/history"+q, nil)
		req.AddCookie(cookie)
		srv.router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("query %s should fall back to default (200), got %d", q, w.Code)
		}
	}
}

func TestPersistenceRestoresCountersAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Port: "8080", DashboardSecret: "x", DashboardMaxAge: time.Hour,
		DataDir: dir, StatsSampleInterval: time.Hour,
	}

	// First "run": drive counters, flush checkpoint, close.
	h1 := hub.NewHub(cfg)
	srv1 := NewServer(cfg, h1)
	if err := srv1.EnablePersistence(); err != nil {
		t.Fatal(err)
	}
	h1.VoteManager.CreateSession("ABC", "t1")
	h1.VoteManager.JoinStagiaire("ABC", "stagiaire001", "Alice")
	h1.VoteManager.StartVote("ABC", "t1", []string{"rouge"}, false, nil, true)
	h1.VoteManager.SubmitVote("ABC", "stagiaire001", []string{"rouge"})
	h1.VoteManager.SubmitVote("ABC", "stagiaire002", []string{"rouge"})
	srv1.FlushStats()
	srv1.CloseStore()

	// Second "run": fresh hub + server, same data dir.
	h2 := hub.NewHub(cfg)
	srv2 := NewServer(cfg, h2)
	if err := srv2.EnablePersistence(); err != nil {
		t.Fatal(err)
	}
	defer srv2.CloseStore()

	snap := h2.VoteManager.Stats().Snapshot()
	if snap.SessionsCreated != 1 {
		t.Errorf("restored SessionsCreated: expected 1, got %d", snap.SessionsCreated)
	}
	if snap.VotesCast != 2 {
		t.Errorf("restored VotesCast: expected 2, got %d", snap.VotesCast)
	}
	if snap.GameEnabledVotes != 1 {
		t.Errorf("restored GameEnabledVotes: expected 1, got %d", snap.GameEnabledVotes)
	}
}

func TestPersistenceDisabledWhenStoreUnopenable(t *testing.T) {
	// Pointing DataDir at a regular file (not a dir) makes store.New fail.
	f, err := os.Create(t.TempDir() + "/afile")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg := &config.Config{
		Port: "8080", DashboardSecret: "x", DashboardMaxAge: time.Hour,
		DataDir: f.Name(), StatsSampleInterval: time.Minute,
	}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)
	if err := srv.EnablePersistence(); err == nil {
		t.Error("EnablePersistence should error when data dir is unusable")
	}
	// Server still serves history as empty (no store wired).
	cookie := loginCookie(t, srv, "x")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard/history", nil)
	req.AddCookie(cookie)
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (empty history), got %d", w.Code)
	}
}
