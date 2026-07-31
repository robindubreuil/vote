package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

// errTestCSPRNGFailure is the deterministic error returned by the randRead
// seam in the R10 cookie-nonce tests. Mirrors security.errTestCSPRNG.
var errTestCSPRNGFailure = errors.New("simulated CSPRNG failure (test)")

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
	srv.SetBuildInfo("test-version", "2026-01-01", "deadbeef")

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
	srv.SetBuildInfo("test-version", "2026-01-01", "deadbeef")

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
		`vote_build_info{version="test-version",build_time="2026-01-01",git_commit="deadbeef"}`,
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
	srv.SetBuildInfo("v", "bt", "gc")

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
		{`vote_build_info{version="v",build_time="bt",git_commit="gc"}`, "build info"},
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
	if got := parsed[`vote_build_info{version="v",build_time="bt",git_commit="gc"}`]; got != 1 {
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
	token := mustSign(t, a, time.Now().Add(time.Hour))
	if !a.verifyCookie(token) {
		t.Error("valid token should verify")
	}

	b := newDashboardAuth("secret-two", time.Hour)
	if b.verifyCookie(token) {
		t.Error("token signed with a different secret must not verify")
	}

	expired := mustSign(t, a, time.Now().Add(-time.Minute))
	if a.verifyCookie(expired) {
		t.Error("expired token must not verify")
	}

	// Each minting produces a unique cookie (nonce). Without this,
	// revoking one login within the same second would revoke them all.
	t2 := mustSign(t, a, time.Now().Add(time.Hour))
	if t2 == token {
		t.Error("two signCookie calls must produce distinct tokens (nonce)")
	}
	if !a.verifyCookie(t2) {
		t.Error("second token should also verify")
	}
}

// mustSign is a test helper around signCookie that fatals on the CSPRNG
// error path (R10) so existing tests keep the happy-path shape after the
// signature gained a second return value.
func mustSign(t *testing.T, a *dashboardAuth, expiresAt time.Time) string {
	t.Helper()
	tok, err := a.signCookie(expiresAt)
	if err != nil {
		t.Fatalf("signCookie: %v", err)
	}
	return tok
}

// TestShouldUseSecureCookie pins the loopback heuristic. S9: the decision
// is driven by the TCP peer (RemoteAddr), not the client-controlled Host
// header, so a spoofed `Host: localhost` cannot flip Secure=false.
//
// S17: behind the documented Caddy deploy TLS terminates at the proxy, so
// the backend sees a plain-HTTP loopback dial (r.TLS nil, RemoteAddr
// 127.0.0.1). Without honouring the proxy's forwarded scheme, every
// production login would carry Secure=false and leak the admin cookie over
// plaintext HTTP. The loopback branch now consults X-Forwarded-Proto /
// Forwarded; the non-loopback branch is unchanged (Secure=true regardless,
// so a remote peer forging the header cannot downgrade).
func TestShouldUseSecureCookie(t *testing.T) {
	cases := []struct {
		name      string
		tls       bool
		remote    string
		host      string
		xfp       string // X-Forwarded-Proto
		forwarded string // Forwarded (RFC 7239)
		expected  bool
	}{
		{"tls", true, "51.0.0.1:1234", "vote.example.com", "", "", true},
		{"loopback v4 plain dev", false, "127.0.0.1:8080", "vote.example.com", "", "", false},
		{"loopback v6 plain dev", false, "[::1]:8080", "vote.example.com", "", "", false},
		{"remote", false, "10.0.0.5:8080", "vote.example.com", "", "", true},
		{"spoofed host does not relax", false, "10.0.0.5:8080", "localhost", "", "", true},
		{"spoofed host v6 does not relax", false, "[2001:db8::1]:8080", "127.0.0.1", "", "", true},

		// S17: Caddy reverse_proxy lands on the loopback branch with
		// X-Forwarded-Proto: https (auto-injected by Caddy). Must set
		// Secure so the cookie can't be stolen over a plaintext hop
		// before the HSTS redirect.
		{"loopback behind https proxy", false, "127.0.0.1:8080", "vote.example.com", "https", "", true},
		{"loopback v6 behind https proxy", false, "[::1]:8080", "vote.example.com", "https", "", true},
		// Case-insensitive header value.
		{"loopback behind HTTPS proxy uppercase", false, "127.0.0.1:8080", "vote.example.com", "HTTPS", "", true},
		// RFC 7239 Forwarded header, quoted and unquoted proto.
		{"loopback behind https proxy via forwarded", false, "127.0.0.1:8080", "vote.example.com", "", `for=203.0.113.7; proto=https; host=vote.example.com`, true},
		{"loopback behind https proxy via forwarded quoted", false, "127.0.0.1:8080", "vote.example.com", "", `proto="https"`, true},
		// Forwarded present but proto=http must NOT flip Secure on.
		{"loopback behind http proxy via forwarded", false, "127.0.0.1:8080", "vote.example.com", "http", "", false},
		{"loopback behind http proxy via forwarded header", false, "127.0.0.1:8080", "vote.example.com", "", `proto=http`, false},
		// No scheme signalled over loopback = genuine local dev.
		{"loopback no scheme stays dev-friendly", false, "127.0.0.1:8080", "localhost", "", "", false},
		// A remote peer forging X-Forwarded-Proto: https is irrelevant —
		// Secure is already true on the non-loopback branch and the
		// header is never consulted there.
		{"remote forging forwarded-proto still true", false, "10.0.0.5:8080", "vote.example.com", "https", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &http.Request{Host: c.host, RemoteAddr: c.remote, Header: http.Header{}}
			if c.tls {
				req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
			}
			if c.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", c.xfp)
			}
			if c.forwarded != "" {
				req.Header.Set("Forwarded", c.forwarded)
			}
			got := shouldUseSecureCookie(req)
			if got != c.expected {
				t.Errorf("tls=%v remote=%s host=%s xfp=%q forwarded=%q: expected %v, got %v", c.tls, c.remote, c.host, c.xfp, c.forwarded, c.expected, got)
			}
		})
	}
}

// TestPurgeRevokedDropsExpiredEntries pins the S4 lazy-eviction contract.
// purgeRevoked runs on every logout and drops revoked entries whose
// recorded time is older than maxAge — the underlying cookie would be
// expired by then anyway, so keeping them wastes memory. Without this
// sweep a long-running server's revocation set would grow unbounded
// (one entry per logout forever). The test backdates some entries past
// the cutoff, leaves others inside the window, and asserts exactly the
// expired subset is reclaimed while fresh entries still block their
// cookies.
func TestPurgeRevokedDropsExpiredEntries(t *testing.T) {
	const maxAge = time.Hour
	a := newDashboardAuth("purge-secret", maxAge)

	now := time.Now()
	// Three revoked cookies: one well past maxAge, one just past, one
	// fresh. We write them directly into the map (bypassing revoke,
	// which stamps time.Now) so we can backdate deterministically.
	a.revoked["old-cookie"] = now.Add(-2 * maxAge)            // 2h ago: expired
	a.revoked["edge-cookie"] = now.Add(-maxAge - time.Second) // 1h+1s: expired
	a.revoked["fresh-cookie"] = now                           // exactly now: within window

	a.purgeRevoked()

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.revoked["old-cookie"]; ok {
		t.Error("entry 2×maxAge old should have been purged")
	}
	if _, ok := a.revoked["edge-cookie"]; ok {
		t.Error("entry just past maxAge should have been purged")
	}
	if _, ok := a.revoked["fresh-cookie"]; !ok {
		t.Error("fresh entry within maxAge must be retained")
	}
	if len(a.revoked) != 1 {
		t.Errorf("expected exactly 1 retained entry, got %d: %+v", len(a.revoked), a.revoked)
	}
}

// TestPurgeRevokedEmptyAndIdempotent asserts the no-op paths: an empty
// set and a set with only-fresh entries both leave the map untouched,
// and calling purge repeatedly is safe (logout fires it every time).
func TestPurgeRevokedEmptyAndIdempotent(t *testing.T) {
	a := newDashboardAuth("purge-secret", time.Hour)

	// Empty set: must not panic, must stay empty.
	a.purgeRevoked()
	if len(a.revoked) != 0 {
		t.Errorf("empty purge should leave 0 entries, got %d", len(a.revoked))
	}

	// Only-fresh set: nothing to evict.
	a.revoked["fresh"] = time.Now()
	a.purgeRevoked()
	a.purgeRevoked() // idempotent repeat
	if len(a.revoked) != 1 {
		t.Errorf("fresh-only set should retain 1 entry after repeated purge, got %d", len(a.revoked))
	}
}

// TestPurgeRevokedRespectsMaxAgeChange pins that the eviction cutoff
// tracks a.maxAge, not a hardcoded constant. An operator who shortens
// VOTE_DASHBOARD_MAX_AGE expects the revocation window to follow (so
// memory frees faster), and a subsequent restart with a longer maxAge
// must not prematurely drop entries that are now within the new window.
func TestPurgeRevokedRespectsMaxAgeChange(t *testing.T) {
	longLived := newDashboardAuth("s", 24*time.Hour)
	longLived.revoked["c"] = time.Now().Add(-2 * time.Hour) // 2h old
	longLived.purgeRevoked()
	longLived.mu.Lock()
	if _, ok := longLived.revoked["c"]; !ok {
		t.Error("2h-old entry must survive under a 24h maxAge")
	}
	longLived.mu.Unlock()

	shortLived := newDashboardAuth("s", 1*time.Hour)
	shortLived.revoked["c"] = time.Now().Add(-2 * time.Hour) // 2h old
	shortLived.purgeRevoked()
	shortLived.mu.Lock()
	if _, ok := shortLived.revoked["c"]; ok {
		t.Error("2h-old entry must be purged under a 1h maxAge")
	}
	shortLived.mu.Unlock()
}

// TestSignCookieFailsOnCSPRNGFailure pins the R10 contract: a kernel CSPRNG
// read failure must surface as an error from signCookie, NOT be swallowed
// into an all-zero nonce. The previous implementation logged the error and
// continued, making signCookie deterministic over (secret, expiry-second)
// so two logins within the same second minted byte-identical cookies and
// defeated the per-token revocation granularity S4 relies on. This mirrors
// B14's fail-loud policy for the rest of the secret-minting surface
// (security.GenerateID / GenerateToken panic on the same condition).
func TestSignCookieFailsOnCSPRNGFailure(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errTestCSPRNGFailure
	}
	t.Cleanup(func() { randRead = orig })

	a := newDashboardAuth("secret", time.Hour)
	tok, err := a.signCookie(time.Now().Add(time.Hour))
	if err == nil {
		t.Fatalf("signCookie should fail on CSPRNG failure, got token %q", tok)
	}
	if tok != "" {
		t.Errorf("signCookie should return empty token on failure, got %q", tok)
	}
}

// TestSignCookieRefusesZeroNonceByDistinctness is the functional guarantee
// R10 protects: under a healthy CSPRNG, two mintings in the SAME second
// must produce distinct cookies (nonce varies). The buggy zero-nonce path
// collapsed them. Runs many iterations within one second to drive the
// "same expiry-second" case hard.
func TestSignCookieRefusesZeroNonceByDistinctness(t *testing.T) {
	a := newDashboardAuth("secret", time.Hour)
	seen := make(map[string]struct{}, 64)
	exp := time.Now().Add(time.Hour)
	for i := 0; i < 64; i++ {
		tok := mustSign(t, a, exp) // same second for most iterations
		if _, dup := seen[tok]; dup {
			t.Fatalf("cookie %q minted twice (nonce not varying — zero-nonce regression)", tok)
		}
		seen[tok] = struct{}{}
	}
}

// TestDashboardLoginCSPRNGFailureReturns500 wires R10 end-to-end through
// the login handler: when the CSPRNG seam fails, POST /dashboard/login
// must respond 500 (and issue NO cookie) rather than minting a zero-nonce
// cookie or panicking. The dashboard is an optional admin feature, so the
// classroom-serving process must stay up while refusing to mint.
func TestDashboardLoginCSPRNGFailureReturns500(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errTestCSPRNGFailure
	}
	t.Cleanup(func() { randRead = orig })

	srv := newTestServer(t, "s3cr3t")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/dashboard/login", strings.NewReader("password=s3cr3t"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when CSPRNG fails, got %d (body=%s)", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == dashboardCookieName {
			t.Errorf("no cookie should be minted on CSPRNG failure, got %s=%s", c.Name, c.Value)
		}
	}
}

// TestSecretMatches is the S18 behavioural contract: the password compare
// accepts exactly the configured secret and rejects everything else,
// regardless of submitted length. Hashing both sides to a fixed 32-byte
// digest before subtle.ConstantTimeCompare is what removes the old
// length-leak, so the assertions deliberately span lengths shorter, equal,
// and longer than the secret.
func TestSecretMatches(t *testing.T) {
	const secret = "correct horse battery staple"
	a := newDashboardAuth(secret, time.Hour)

	cases := []struct {
		name string
		got  string
		want bool
	}{
		{"exact", secret, true},
		{"empty", "", false},
		{"prefix only", "correct horse", false},
		{"trailing newline", secret + "\n", false},
		{"shorter wrong", "nope", false},
		{"same length wrong", "wrong!!horse!!battery!!staple", false},
		{"longer wrong", secret + " extra junk that is much longer than the real secret", false},
		{"case differs", "Correct Horse Battery Staple", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := a.secretMatches(c.got); got != c.want {
				t.Errorf("secretMatches(%q) = %v, want %v", c.got, got, c.want)
			}
		})
	}
}

// TestSecretMatchesNoLengthOracle is the S18 timing property. The original
// bug was a length oracle: subtle.ConstantTimeCompare short-circuits on a
// length mismatch, so a probe whose length EQUALS the secret's took the full
// constant-time sweep (slow) while a probe of any other length returned
// immediately (fast). Iterating probe lengths and watching for the spike
// recovered len(secret).
//
// After hashing both sides the secret's length no longer gates any branch —
// the input is hashed (cost ∝ len(input), which the attacker already knows)
// and the secret is hashed once (a constant offset). The discriminating
// signal between the buggy and fixed code is therefore the RELATIVE cost of
// an at-length probe vs a longer off-length probe:
//   - buggy:  at-length probe is SLOWER than a longer probe (sweep vs short-
//     circuit), so ratio = at-length/longer >> 1.
//   - fixed:  at-length probe is FASTER than a longer probe (hashes fewer
//     bytes), so ratio = at-length/longer < 1.
//
// We assert the at-length probe is not slower than a longer probe by more
// than a small factor. This never fails spuriously on the fixed code (the
// at-length probe genuinely hashes strictly fewer bytes) and fails reliably
// on the buggy code (the at-length probe hits the slow sweep). A starved CI
// box inflates both means similarly, leaving the ratio < 1, so the assertion
// is one-sided-safe: it can fail to catch a regression under load but never
// spuriously fails.
func TestSecretMatchesNoLengthOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("timing property test omitted in -short mode")
	}
	const secret = "a-secret-of-known-length" // len 25
	a := newDashboardAuth(secret, time.Hour)

	atLength := strings.Repeat("y", len(secret)) // wrong but len == len(secret)
	longer := strings.Repeat("x", 4096)          // wrong, len != len(secret)

	const iterations = 200_000
	atLengthMean := meanCallDuration(a.secretMatches, atLength, iterations)
	longerMean := meanCallDuration(a.secretMatches, longer, iterations)

	// At-length must not be dramatically slower than longer. The buggy
	// code makes it ~5-10x slower; the fixed code makes it faster. A 1.5x
	// factor absorbs scheduling noise while still rejecting the oracle.
	if atLengthMean > longerMean*3/2 {
		t.Fatalf("secretMatches length oracle present: at-length probe mean=%v > longer probe mean=%v (expected at-length not slower)", atLengthMean, longerMean)
	}
}

// meanCallDuration runs fn n times against got and returns the mean wall-
// clock duration per call. Warmup calls prime caches before measurement.
func meanCallDuration(fn func(string) bool, got string, n int) time.Duration {
	const warmup = 2000
	for i := 0; i < warmup; i++ {
		fn(got)
	}
	start := time.Now()
	for i := 0; i < n; i++ {
		fn(got)
	}
	return time.Since(start) / time.Duration(n)
}
