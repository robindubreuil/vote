package server

import (
	"crypto/tls"
	"fmt"
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
	// Valid-looking but wrong signature. Also uses the old 3-part format
	// (pre-nonce) which the parser must reject outright.
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: "v1.99999999999.aGVsbG8"})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected redirect for tampered cookie, got %d", w.Code)
	}
}

// TestDashboardOldFormatCookieRejected verifies that cookies minted before
// the S4 nonce change (3-part format) are rejected, forcing a re-login
// rather than silently accepting a pre-revocation cookie.
func TestDashboardOldFormatCookieRejected(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	// Correct version, valid-looking 3-part shape — must be rejected
	// because the parser now requires 4 parts (v1.nonce.exp.sig).
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: "v1.99999999999.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected redirect for old-format cookie, got %d", w.Code)
	}
}

// TestDashboardLogoutClearsCookie verifies the cookie is cleared client-side.
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

// TestDashboardLogoutRevokesCookieServerSide covers S4: after logout, the
// exfiltrated cookie value must stop working server-side immediately, not
// at the embedded expiry. Without revocation, a stolen cookie remains
// valid up to maxAge (7 days default).
func TestDashboardLogoutRevokesCookieServerSide(t *testing.T) {
	srv := newTestServer(t, "s3cr3t")

	// 1. Mint a valid cookie.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/login", strings.NewReader("password=s3cr3t"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)

	var token string
	for _, c := range w.Result().Cookies() {
		if c.Name == "vote_admin" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("login should mint a cookie")
	}

	// 2. Cookie works before logout.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: token})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cookie should work before logout, got %d", w.Code)
	}

	// 3. Logout revokes the cookie server-side.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/dashboard/logout", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: token})
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("logout should redirect, got %d", w.Code)
	}

	// 4. The SAME cookie value is now rejected (would-be valid by
	// signature/expiry alone, but the revocation set blocks it).
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: token})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("revoked cookie must be rejected (redirect to login), got %d", w.Code)
	}

	// 5. A freshly minted cookie still works (revocation is per-token, not global).
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/dashboard/login", strings.NewReader("password=s3cr3t"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)
	var token2 string
	for _, c := range w.Result().Cookies() {
		if c.Name == "vote_admin" {
			token2 = c.Value
		}
	}
	if token2 == "" || token2 == token {
		t.Fatal("second login should mint a distinct cookie")
	}
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "vote_admin", Value: token2})
	req.Header.Set("Accept", "text/html")
	srv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("fresh cookie should work after a different token was revoked, got %d", w.Code)
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
	mgr.JoinStagiaire("ABC", "stagiaire001", "Alice", "")
	mgr.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false)
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

// TestMetricsEndpointLabeledSeriesExposition is a regression guard for the
// dashboard's in-browser Prometheus parser. The parser must survive every
// labeled line the server emits (histogram buckets, sessions_by_state,
// build_info) — and the spec calls for exactly one HELP and one TYPE per
// metric name. We assert both here against the raw text so a future change
// to writeGaugeWithLabels or writeHistogram can't silently resurrect the
// "drop every labeled line" parser bug.
func TestMetricsEndpointLabeledSeriesExposition(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	mgr := h.VoteManager
	mgr.CreateSession("ABC", "trainer1")
	mgr.JoinStagiaire("ABC", "stagiaire001", "Alice", "")
	mgr.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false)
	mgr.SubmitVote("ABC", "stagiaire001", []string{"rouge"})
	mgr.RemoveSession("ABC") // observe one ended session in every histogram

	srv := NewServer(cfg, h)
	srv.SetBuildInfo("test-version", "2026-01-01")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	srv.router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	// Every labeled series that the dashboard parser must extract. Assert
	// presence of the labeled KEY (not the numeric value — bucket values
	// depend on the actual session duration at runtime).
	requiredLabeledKeys := []string{
		`vote_sessions_by_state{state="idle"}`,
		`vote_sessions_by_state{state="active"}`,
		`vote_sessions_by_state{state="closed"}`,
		`vote_session_duration_seconds_bucket{le="60"}`,
		`vote_session_duration_seconds_bucket{le="300"}`,
		`vote_session_duration_seconds_bucket{le="+Inf"}`,
		`vote_session_duration_seconds_count`,
		`vote_votes_per_session_bucket{le="0"}`,
		`vote_votes_per_session_bucket{le="1"}`,
		`vote_votes_per_session_bucket{le="+Inf"}`,
		`vote_votes_per_session_count`,
		`vote_trainees_per_session_bucket{le="1"}`,
		`vote_trainees_per_session_bucket{le="+Inf"}`,
		`vote_trainees_per_session_count`,
		`vote_build_info{version="test-version",build_time="2026-01-01"}`,
	}
	for _, key := range requiredLabeledKeys {
		// Match the key as the start of a whitespace-or-value-terminated token
		// so that "le=\"1\"" doesn't false-match "le=\"10\"". Each Prometheus
		// line is `<key> <number>` so the key is followed by ' ' or end of line.
		if !lineStartsWith(body, key) {
			t.Errorf("metrics body missing labeled key %q\nBody:\n%s", key, body)
		}
	}

	// Prometheus spec: HELP and TYPE appear exactly once per metric name.
	// Duplicating them (the old writeGaugeWithLabels-in-a-loop bug) is
	// tolerated by lenient scrapers but breaks strict ones and bloats output.
	helpCounts := map[string]int{}
	typeCounts := map[string]int{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# HELP ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				helpCounts[parts[2]]++
			}
		} else if strings.HasPrefix(line, "# TYPE ") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				typeCounts[parts[2]]++
			}
		}
	}
	for name, n := range helpCounts {
		if n > 1 {
			t.Errorf("metric %s: HELP emitted %d times (spec allows exactly 1)", name, n)
		}
	}
	for name, n := range typeCounts {
		if n > 1 {
			t.Errorf("metric %s: TYPE emitted %d times (spec allows exactly 1)", name, n)
		}
	}
}

// TestDashboardParseMetricsHandlesLabeledLines is a Go-side reference
// implementation of the in-browser parseMetrics. It exists so a regression
// in the JS parser is caught at the next test run — the JS itself can't be
// executed here without pulling in a JS engine, so we mirror its exact rules
// and assert that feeding real /metrics output to those rules yields the
// labeled keys the dashboard depends on. If you change parseMetrics in
// dashboard.go, update this Go mirror in lock-step.
func TestDashboardParseMetricsHandlesLabeledLines(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	h := hub.NewHub(cfg)
	mgr := h.VoteManager
	mgr.CreateSession("ABC", "trainer1")
	mgr.JoinStagiaire("ABC", "stagiaire001", "Alice", "")
	mgr.StartVote("ABC", "trainer1", []string{"rouge"}, false, nil, false, false, false)
	mgr.SubmitVote("ABC", "stagiaire001", []string{"rouge"})
	mgr.RemoveSession("ABC")

	srv := NewServer(cfg, h)
	srv.SetBuildInfo("v", "bt")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	srv.router.ServeHTTP(w, req)
	body := w.Body.String()

	parsed := parseMetricsReference(body)

	cases := []struct{ key, desc string }{
		{`vote_sessions_by_state{state="idle"}`, "idle state series"},
		{`vote_sessions_by_state{state="active"}`, "active state series"},
		{`vote_sessions_by_state{state="closed"}`, "closed state series"},
		{`vote_session_duration_seconds_bucket{le="60"}`, "duration bucket le=60"},
		{`vote_session_duration_seconds_bucket{le="+Inf"}`, "duration bucket +Inf"},
		{`vote_session_duration_seconds_count`, "duration count"},
		{`vote_votes_per_session_bucket{le="1"}`, "votes bucket le=1"},
		{`vote_votes_per_session_bucket{le="+Inf"}`, "votes bucket +Inf"},
		{`vote_votes_per_session_count`, "votes count"},
		{`vote_trainees_per_session_bucket{le="1"}`, "trainees bucket le=1"},
		{`vote_build_info{version="v",build_time="bt"}`, "build info"},
		{`vote_sessions_created_total`, "unlabeled counter"},
	}
	for _, c := range cases {
		v, ok := parsed[c.key]
		if !ok {
			t.Errorf("parser dropped labeled key %q (%s) — this is exactly the parseFloat-on-labels bug", c.key, c.desc)
			continue
		}
		if v < 0 {
			t.Errorf("parser returned negative for %q: %v", c.key, v)
		}
	}
	// Specific value assertions: the ended session observed exactly one
	// duration sample, one votes sample (=1 vote), one trainees sample (=1 trainee).
	if got := parsed[`vote_session_duration_seconds_count`]; got != 1 {
		t.Errorf("duration count: expected 1, got %v", got)
	}
	if got := parsed[`vote_session_duration_seconds_bucket{le="+Inf"}`]; got != 1 {
		t.Errorf("duration +Inf bucket: expected 1, got %v", got)
	}
	if got := parsed[`vote_votes_per_session_bucket{le="1"}`]; got != 1 {
		t.Errorf("votes bucket le=1: expected 1, got %v", got)
	}
	if got := parsed[`vote_votes_per_session_bucket{le="+Inf"}`]; got != 1 {
		t.Errorf("votes bucket +Inf: expected 1, got %v", got)
	}
	if got := parsed[`vote_build_info{version="v",build_time="bt"}`]; got != 1 {
		t.Errorf("build_info value: expected 1, got %v", got)
	}
}

// parseMetricsReference mirrors dashboard.go's parseMetrics exactly. Any
// change to the JS parser MUST be reflected here. The dashboard runs in the
// browser, so this Go twin is the only compile-time-checkable spec we have.
func parseMetricsReference(text string) map[string]float64 {
	out := make(map[string]float64)
	for _, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var name, rest, labels string
		brace := strings.Index(line, "{")
		if brace > -1 {
			close := strings.Index(line, "}")
			if close < 0 {
				continue
			}
			name = line[:brace]
			labels = "{" + line[brace+1:close] + "}"
			rest = strings.TrimSpace(line[close+1:])
		} else {
			sp := strings.Index(line, " ")
			name = line[:sp]
			rest = line[sp+1:]
		}
		var val float64
		if _, err := fmt.Sscanf(rest, "%g", &val); err != nil {
			continue
		}
		out[name+labels] = val
	}
	return out
}

// lineStartsWith reports whether any line of body begins with prefix followed
// by either a space or end-of-line. Used so a key like "le=1" doesn't
// accidentally match "le=10".
func lineStartsWith(body, prefix string) bool {
	for _, line := range strings.Split(body, "\n") {
		if line == prefix || strings.HasPrefix(line, prefix+" ") {
			return true
		}
	}
	return false
}

// TestCookieSigningRoundTrip is a focused unit test on the HMAC scheme:
// sign then verify, confirm a different secret rejects, and confirm each
// minting is unique (nonce).
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

	// Each minting produces a unique cookie (nonce). Without this,
	// revoking one login within the same second would revoke them all.
	t2 := a.signCookie(time.Now().Add(time.Hour))
	if t2 == token {
		t.Error("two signCookie calls must produce distinct tokens (nonce)")
	}
	if !a.verifyCookie(t2) {
		t.Error("second token should also verify")
	}
}

// TestShouldUseSecureCookie pins the loopback heuristic. S9: the decision
// is driven by the TCP peer (RemoteAddr), not the client-controlled Host
// header, so a spoofed `Host: localhost` cannot flip Secure=false.
func TestShouldUseSecureCookie(t *testing.T) {
	cases := []struct {
		name     string
		tls      bool
		remote   string
		host     string
		expected bool
	}{
		{"tls", true, "51.0.0.1:1234", "vote.example.com", true},
		{"loopback v4", false, "127.0.0.1:8080", "vote.example.com", false},
		{"loopback v6", false, "[::1]:8080", "vote.example.com", false},
		{"remote", false, "10.0.0.5:8080", "vote.example.com", true},
		{"spoofed host does not relax", false, "10.0.0.5:8080", "localhost", true},
		{"spoofed host v6 does not relax", false, "[2001:db8::1]:8080", "127.0.0.1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &http.Request{Host: c.host, RemoteAddr: c.remote}
			if c.tls {
				req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
			}
			got := shouldUseSecureCookie(req)
			if got != c.expected {
				t.Errorf("tls=%v remote=%s host=%s: expected %v, got %v", c.tls, c.remote, c.host, c.expected, got)
			}
		})
	}
}
