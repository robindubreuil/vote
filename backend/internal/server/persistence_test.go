package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	mgr.JoinStagiaire("ABC", "stagiaire001", "Alice", "")
	mgr.StartVote("ABC", "t1", []string{"rouge"}, false, nil, false, false, false)
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
	h1.VoteManager.JoinStagiaire("ABC", "stagiaire001", "Alice", "")
	h1.VoteManager.StartVote("ABC", "t1", []string{"rouge"}, false, nil, true, false, false)
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

// TestPersistenceRestoresHistogramsAcrossRestart is the regression test for
// the bug where the dashboard showed "aucune session terminée" right after a
// restart even though counters showed many sessions: histograms weren't
// restored (intentional at the time) while counters were. Now both are
// persisted in counters.json and replayed via ProductStats.Restore.
func TestPersistenceRestoresHistogramsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Port: "8080", DashboardSecret: "x", DashboardMaxAge: time.Hour,
		DataDir: dir, StatsSampleInterval: time.Hour,
	}

	// First run: end three sessions with known vote/trainee counts so the
	// histogram observations are deterministic. Then flush and close.
	h1 := hub.NewHub(cfg)
	srv1 := NewServer(cfg, h1)
	if err := srv1.EnablePersistence(); err != nil {
		t.Fatal(err)
	}
	codes := []string{"AAA", "BCD", "EFG"}
	for i, votes := range []int{1, 5, 11} {
		code := codes[i]
		h1.VoteManager.CreateSession(code, "t1")
		for v := 0; v < votes; v++ {
			id := fmt.Sprintf("s%03d", v)
			h1.VoteManager.JoinStagiaire(code, id, "name", "")
			h1.VoteManager.StartVote(code, "t1", []string{"rouge"}, false, nil, false, false, false)
			h1.VoteManager.SubmitVote(code, id, []string{"rouge"})
		}
		h1.VoteManager.RemoveSession(code) // observe one ended session
	}
	before := h1.VoteManager.Stats().Snapshot()
	if before.VotesPerSession.Count != 3 {
		t.Fatalf("setup: expected 3 ended sessions, got %d", before.VotesPerSession.Count)
	}
	srv1.FlushStats()
	srv1.CloseStore()

	// Second run: fresh hub + server on the same data dir. The histograms
	// must come back with the same observation count, sum, and per-bucket
	// distribution as the first run had at shutdown.
	h2 := hub.NewHub(cfg)
	srv2 := NewServer(cfg, h2)
	if err := srv2.EnablePersistence(); err != nil {
		t.Fatal(err)
	}
	defer srv2.CloseStore()

	after := h2.VoteManager.Stats().Snapshot()
	if after.VotesPerSession.Count != before.VotesPerSession.Count {
		t.Errorf("VotesPerSession.Count: expected %d, got %d",
			before.VotesPerSession.Count, after.VotesPerSession.Count)
	}
	if after.VotesPerSession.Sum != before.VotesPerSession.Sum {
		t.Errorf("VotesPerSession.Sum: expected %v, got %v",
			before.VotesPerSession.Sum, after.VotesPerSession.Sum)
	}
	if len(after.VotesPerSession.Buckets) != len(before.VotesPerSession.Buckets) {
		t.Fatalf("VotesPerSession bucket count mismatch: before=%d after=%d",
			len(before.VotesPerSession.Buckets), len(after.VotesPerSession.Buckets))
	}
	for i := range before.VotesPerSession.Buckets {
		ab, bb := after.VotesPerSession.Buckets[i], before.VotesPerSession.Buckets[i]
		if ab.LE != bb.LE || ab.Count != bb.Count {
			t.Errorf("VotesPerSession bucket %d (le=%v): before=%d after=%d",
				i, bb.LE, bb.Count, ab.Count)
		}
	}

	// Ending a new session in run 2 must accumulate on top of the restored
	// distribution, not reset it.
	h2.VoteManager.CreateSession("NEW", "t1")
	h2.VoteManager.JoinStagiaire("NEW", "s001", "n", "")
	h2.VoteManager.StartVote("NEW", "t1", []string{"rouge"}, false, nil, false, false, false)
	h2.VoteManager.SubmitVote("NEW", "s001", []string{"rouge"})
	h2.VoteManager.RemoveSession("NEW")
	combined := h2.VoteManager.Stats().Snapshot()
	if combined.VotesPerSession.Count != before.VotesPerSession.Count+1 {
		t.Errorf("post-restore Observe: expected count %d, got %d",
			before.VotesPerSession.Count+1, combined.VotesPerSession.Count)
	}
}

// TestPersistenceRestoresCountersAcrossRestartBackwardCompat verifies a
// counters.json written by an older binary (one that lacked histogram fields)
// still loads and restores the counters, leaving histograms at zero.
func TestPersistenceRestoresCountersAcrossRestartBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	legacyJSON := `{"ts":"2025-01-01T00:00:00Z","sc":42,"vs":99,"vc":500,"tj":120,"ge":10,"mc":3}`
	if err := os.WriteFile(filepath.Join(dir, "counters.json"), []byte(legacyJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create the data dir first so store.New is happy.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Port: "8080", DashboardSecret: "x", DashboardMaxAge: time.Hour,
		DataDir: dir, StatsSampleInterval: time.Hour,
	}
	h := hub.NewHub(cfg)
	srv := NewServer(cfg, h)
	if err := srv.EnablePersistence(); err != nil {
		t.Fatal(err)
	}
	defer srv.CloseStore()

	snap := h.VoteManager.Stats().Snapshot()
	if snap.SessionsCreated != 42 {
		t.Errorf("legacy SessionsCreated: expected 42, got %d", snap.SessionsCreated)
	}
	if snap.VotesCast != 500 {
		t.Errorf("legacy VotesCast: expected 500, got %d", snap.VotesCast)
	}
	if snap.VotesPerSession.Count != 0 {
		t.Errorf("legacy histograms should be zero, got VotesPerSession.Count=%d", snap.VotesPerSession.Count)
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
