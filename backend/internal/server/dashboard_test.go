package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

func newTestServer(t *testing.T, secret string) *Server {
	t.Helper()
	cfg := &config.Config{
		Port:            "8080",
		DashboardSecret: secret,
		DashboardMaxAge: time.Hour,
	}
	h := hub.NewHub(cfg)
	return NewServer(cfg, h)
}

func TestDashboardDisabledWhenNoSecret(t *testing.T) {
	srv := newTestServer(t, "")

	for _, path := range []string{"/dashboard", "/dashboard/login", "/dashboard/logout"} {
		w := httptest.NewRecorder()
		method := "GET"
		if path == "/dashboard/logout" {
			method = "POST"
		}
		req, _ := http.NewRequest(method, path, nil)
		srv.router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404 when secret unset, got %d", method, path, w.Code)
		}
	}
}

func TestDashboardLoginGetReturnsForm(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard/login", nil)
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "name=\"password\"") {
		t.Error("login page should contain a password field")
	}
}

func TestDashboardLoginWrongPassword(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/login", strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", w.Code)
	}
}

func TestDashboardLoginCorrectPasswordSetsCookie(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/login", strings.NewReader("password=s3cr3t"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect on success, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/dashboard" {
		t.Errorf("expected redirect to /dashboard, got %q", w.Header().Get("Location"))
	}
	setCookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "vote_admin=") {
		t.Errorf("expected vote_admin cookie, got: %s", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Error("cookie must be HttpOnly")
	}
	if !strings.Contains(setCookie, "SameSite=Strict") {
		t.Error("cookie must be SameSite=Strict")
	}
}

func TestDashboardRequiresAuth(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")

	// No cookie → redirect to login (HTML Accept).
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/dashboard/login" {
		t.Errorf("unauthed HTML request: expected 302 to /dashboard/login, got %d %s", w.Code, w.Header().Get("Location"))
	}

	// No cookie, XHR → 401 JSON.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/dashboard", nil)
	req.Header.Set("Accept", "application/json")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthed XHR: expected 401, got %d", w.Code)
	}
}

func TestDashboardAccessibleWithValidCookie(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")

	// Mint a cookie by logging in.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/login", strings.NewReader("password=s3cr3t"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var token string
	for _, c := range cookies {
		if c.Name == "vote_admin" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no vote_admin cookie set")
	}

	// Use the cookie to access the dashboard.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: token})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid cookie, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Tableau de bord") {
		t.Error("dashboard HTML not served")
	}
	// Regression: logout must be a POST form, not a GET <a> (GET logout is a
	// CSRF vector and the route is POST-only).
	if !strings.Contains(body, `method="POST" action="/dashboard/logout"`) {
		t.Error("dashboard must contain a POST logout form")
	}
}

func TestDashboardTamperedCookieRejected(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	// Valid-looking but wrong signature.
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: "v1.99999999999.aGVsbG8"})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected redirect for tampered cookie, got %d", w.Code)
	}
}

func TestDashboardLogoutClearsCookie(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/logout", nil)
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	setCookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "vote_admin=") {
		t.Errorf("logout should clear vote_admin cookie, got: %s", setCookie)
	}
	if !strings.Contains(strings.ToLower(setCookie), "max-age=0") {
		t.Errorf("logout cookie should have Max-Age=0, got: %s", setCookie)
	}
}

// TestMetricsEndpointProductCounters verifies the new product counters and
// histograms are exposed in Prometheus format.
func TestMetricsEndpointProductCounters(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	// Drive some counters via the manager to ensure non-zero wiring.
	mgr := h.VoteManager
	mgr.CreateSession("ABC", "trainer1")
	mgr.JoinStagiaire("ABC", "stagiaire001", "Alice")
	mgr.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false)
	mgr.SubmitVote("ABC", "stagiaire001", []string{"rouge"})

	srv := NewServer(cfg, h)
	srv.SetBuildInfo("test-version", "2026-01-01")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	srv.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	expected := []string{
		"# TYPE vote_sessions_created_total counter",
		"vote_sessions_created_total 1",
		"# TYPE vote_votes_cast_total counter",
		"vote_votes_cast_total 1",
		"# TYPE vote_trainees_joined_total counter",
		"vote_trainees_joined_total 1",
		"# TYPE vote_votes_started_total counter",
		"vote_votes_started_total 1",
		"# TYPE vote_session_duration_seconds histogram",
		"vote_session_duration_seconds_bucket{le=\"+Inf\"}",
		"vote_session_duration_seconds_count",
		"vote_session_duration_seconds_sum",
		"# TYPE vote_votes_per_session histogram",
		"vote_votes_per_session_bucket{le=\"+Inf\"}",
		"# TYPE vote_trainees_per_session histogram",
	}
	for _, e := range expected {
		if !strings.Contains(body, e) {
			t.Errorf("metrics body missing %q\nBody:\n%s", e, body)
		}
	}
}

// TestCookieSigningRoundTrip is a focused unit test on the HMAC scheme:
// sign then verify, and confirm a different secret rejects.
func TestCookieSigningRoundTrip(t *testing.T) {
	a := newDashboardAuth("secret-one", time.Hour)
	token := a.signCookie(time.Now().Add(time.Hour))
	if !a.verifyCookie(token) {
		t.Error("valid token should verify")
	}

	b := newDashboardAuth("secret-two", time.Hour)
	if b.verifyCookie(token) {
		t.Error("token signed with a different secret must not verify")
	}

	expired := a.signCookie(time.Now().Add(-time.Minute))
	if a.verifyCookie(expired) {
		t.Error("expired token must not verify")
	}
}

// TestShouldUseSecureCookie pins the localhost heuristic.
func TestShouldUseSecureCookie(t *testing.T) {
	cases := []struct {
		tls      bool
		host     string
		expected bool
	}{
		{true, "example.com", true},
		{false, "localhost:8080", false},
		{false, "127.0.0.1:8080", false},
		{false, "vote.example.com", true},
		{false, "10.0.0.5:8080", true},
	}
	for _, c := range cases {
		req := &http.Request{Host: c.host}
		if c.tls {
			req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		}
		got := shouldUseSecureCookie(req)
		if got != c.expected {
			t.Errorf("host=%s tls=%v: expected %v, got %v", c.host, c.tls, c.expected, got)
		}
	}
}
