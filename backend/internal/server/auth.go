package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// randRead is the package-level CSPRNG seam, mirroring security.randRead so
// the cookie-nonce failure path can be exercised without provoking a real
// kernel-entropy fault. Production code never reassigns it; only tests do.
var randRead = rand.Read

const (
	dashboardCookieName = "vote_admin"
	dashboardCookieVal  = "v1"
	// dashboardNonceBytes makes each minted cookie unique even when the
	// expiry lands in the same second. Without it, signCookie is
	// deterministic over (secret, expiry) and two logins within one
	// second produce identical tokens — so revoking one would revoke
	// both, and the S4 revocation test can't distinguish them.
	dashboardNonceBytes = 16

	// dashboardHistoryDefaultLimit is the default tail length returned
	// by /dashboard/history when no ?limit is supplied. Sized to one
	// week of 5-min samples (7 days × 288 samples/day = 2016): long
	// enough to spot weekly usage cycles, bounded so the JSON payload
	// stays well under a megabyte even on a long-running server. The
	// client-side seed (dashboard.go seedFromServer) requests the same
	// count so the two stay in sync — keep both updated together.
	dashboardHistoryDefaultLimit = 2016

	// dashboardHistoryMaxLimit is the hard cap applied to any client-
	// supplied ?limit. 20000 samples ≈ 69 days at 5-min cadence, which
	// bounds the worst-case response to a few MB regardless of how far
	// back the operator scrolls the trend chart. Above this the
	// dashboard switches to the /dashboard/history scrollback UI rather
	// than loading everything in one request.
	dashboardHistoryMaxLimit = 20000

	// dashboardLoginRetryAfter is the seconds value sent in the
	// Retry-After header when the login endpoint is throttled by the
	// per-IP join-rate limiter. Matches the limiter's windowed backoff
	// (a failed attempt under backoff rejects for ≥1s and up to 5min);
	// 60s is the conservative mid-point a browser or curl will honour
	// before retrying, keeping dash-key brute-force attempts rate-
	// limited at the same cadence as session-code enumeration.
	dashboardLoginRetryAfter = "60"
)

// dashboardAuth holds the configuration for the cookie auth scheme. A zero
// value (empty secret) means the dashboard is disabled.
//
// S4: a server-side revocation set backs logout. Without it, an exfiltrated
// cookie stays valid until its embedded expiry (up to 7 days). Revoked
// entries are keyed by the full cookie value and expire naturally at
// maxAge, bounding the set's memory.
type dashboardAuth struct {
	secret  []byte
	maxAge  time.Duration
	mu      sync.Mutex
	revoked map[string]time.Time
}

func newDashboardAuth(secret string, maxAge time.Duration) *dashboardAuth {
	if secret == "" {
		return nil
	}
	return &dashboardAuth{secret: []byte(secret), maxAge: maxAge, revoked: make(map[string]time.Time)}
}

func (a *dashboardAuth) enabled() bool { return a != nil }

// secretMatches reports whether the submitted password equals the configured
// dashboard secret, using a constant-time comparison.
//
// S18: the prior direct subtle.ConstantTimeCompare([]byte(got), want) is only
// constant-time for equal lengths — a length mismatch returns 0 immediately,
// leaking the secret length by timing. Hashing both sides first normalises
// every probe to a fixed 32-byte digest, so the subsequent compare is always
// over equal lengths and fully constant-time. CheckJoinRateLimit("dash:"+IP)
// already throttles probing to ~3/10min per IP, so the leak is low-impact on
// its own; this closes the one spot the codebase's constant-time discipline
// slipped.
func (a *dashboardAuth) secretMatches(got string) bool {
	gotSum := sha256.Sum256([]byte(got))
	wantSum := sha256.Sum256(a.secret)
	return subtle.ConstantTimeCompare(gotSum[:], wantSum[:]) == 1
}

// signCookie builds an HMAC-SHA256 over the payload (version + nonce +
// expiry) and returns "payload.signature" both base64url-encoded. The nonce
// makes each minting unique; the signature binds the expiry so a leaked
// cookie cannot have its lifetime extended without the secret.
//
// R10: the nonce is sourced from the kernel CSPRNG and a read failure is
// propagated rather than swallowed. The previous implementation logged the
// error and continued with an all-zero nonce, which made signCookie
// deterministic over (secret, expiry-second): two logins within the same
// second minted byte-identical cookies, defeating the per-token revocation
// granularity S4 depends on. This is consistent with B14's "fail loud"
// policy for the rest of the secret-minting surface (GenerateID /
// GenerateToken panic on the same condition); the HTTP handler turns the
// error into a 500 instead of a panic so the classroom-serving process
// stays up.
func (a *dashboardAuth) signCookie(expiresAt time.Time) (string, error) {
	nonce := make([]byte, dashboardNonceBytes)
	if _, err := randRead(nonce); err != nil {
		return "", fmt.Errorf("dashboard cookie nonce: %w", err)
	}
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	payload := dashboardCookieVal + "." + hex.EncodeToString(nonce) + "." + exp
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

// verifyCookie validates the signature, the expiry, AND that the cookie has
// not been revoked server-side. Returns true only when all three hold.
// Format: v1.<nonce>.<expUnix>.<sig> (4 dot-separated parts).
func (a *dashboardAuth) verifyCookie(raw string) bool {
	if raw == "" {
		return false
	}
	parts := strings.SplitN(raw, ".", 4)
	if len(parts) != 4 {
		return false
	}
	payload := parts[0] + "." + parts[1] + "." + parts[2]
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	wantSig := mac.Sum(nil)
	if !hmac.Equal(gotSig, wantSig) {
		return false
	}
	if parts[0] != dashboardCookieVal {
		return false
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return false
	}
	if !time.Now().Before(time.Unix(expUnix, 0)) {
		return false
	}
	// S4: reject cookies that were revoked via /dashboard/logout.
	a.mu.Lock()
	_, revoked := a.revoked[raw]
	a.mu.Unlock()
	return !revoked
}

// revoke adds the presented cookie value to the revocation set so subsequent
// presentations are rejected even though the signature and expiry would
// otherwise still pass.
func (a *dashboardAuth) revoke(raw string) {
	if raw == "" {
		return
	}
	a.mu.Lock()
	a.revoked[raw] = time.Now()
	a.mu.Unlock()
}

// purgeRevoked drops entries older than maxAge — the underlying cookie would
// be expired by then anyway, so keeping them wastes memory. Called lazily on
// revoke to amortize the scan.
func (a *dashboardAuth) purgeRevoked() {
	cutoff := time.Now().Add(-a.maxAge)
	a.mu.Lock()
	for k, t := range a.revoked {
		if t.Before(cutoff) {
			delete(a.revoked, k)
		}
	}
	a.mu.Unlock()
}

// shouldUseSecureCookie returns true when the connection is encrypted (TLS) or
// is not loopback. On plain-HTTP loopback (local dev) Secure is relaxed so the
// browser actually persists the cookie; production behind TLS always sets it.
//
// S9: the loopback check uses RemoteAddr (set by the server from the actual
// TCP peer) rather than the Host header, which is fully client-controlled.
// An attacker who can inject a `Host: localhost` header over plain HTTP
// would otherwise flip Secure=false and enable cookie theft over an
// insecure hop.
//
// S17: behind the recommended Caddy deploy, TLS terminates at the proxy and
// the backend sees a plain-HTTP loopback dial (r.TLS is nil, RemoteAddr is
// 127.0.0.1) — so the loopback branch alone would return Secure=false for
// every production login, leaking the admin cookie over plaintext HTTP to a
// MITM who induces any http:// request before the HSTS redirect. The fix
// honours the proxy's forwarded scheme inside the loopback branch: a
// loopback peer means the dialer is the local proxy (Caddy's reverse_proxy
// auto-injects X-Forwarded-Proto), so trusting it errs toward the safe
// Secure=true. Genuine local dev runs without a proxy, so the header is
// absent and the dev-friendly Secure=false is preserved.
func shouldUseSecureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if isLoopbackRemoteAddr(r.RemoteAddr) {
		if forwardedSchemeIsHTTPS(r) {
			return true
		}
		return false
	}
	return true
}

// forwardedSchemeIsHTTPS reports whether an inbound loopback request was
// fronted by an HTTPS proxy. Checks the de-facto X-Forwarded-Proto header
// and the standard Forwarded header (RFC 7239), both case-insensitively.
// Only consulted inside the loopback branch of shouldUseSecureCookie, so it
// never governs a request whose TCP peer is a non-local client.
func forwardedSchemeIsHTTPS(r *http.Request) bool {
	if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}
	fwd := r.Header.Get("Forwarded")
	if fwd == "" {
		return false
	}
	for _, part := range strings.Split(fwd, ";") {
		if k, v, ok := strings.Cut(strings.TrimSpace(part), "="); ok && strings.EqualFold(k, "proto") {
			if strings.EqualFold(strings.Trim(v, `"`), "https") {
				return true
			}
		}
	}
	return false
}

// isLoopbackRemoteAddr reports whether the TCP peer is a loopback address.
func isLoopbackRemoteAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// requireAuth is the middleware protecting /dashboard. If the dashboard is
// disabled (no secret configured) every request 404s. Authenticated requests
// pass through; others redirect to the login page (browser) or 401 (XHR).
func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.auth.enabled() {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		cookie, err := c.Cookie(dashboardCookieName)
		if err == nil && s.auth.verifyCookie(cookie) {
			c.Next()
			return
		}
		if wantsHTML(c.Request) {
			c.Redirect(http.StatusFound, "/dashboard/login")
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		}
	}
}

// handleDashboardLogin serves the GET (login form) and POST (verify + set
// cookie). On POST, the password is compared in constant time to the
// configured secret; on success a signed cookie is issued.
func (s *Server) handleDashboardLogin(c *gin.Context) {
	if !s.auth.enabled() {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if c.Request.Method == http.MethodGet {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(loginPageHTML))
		return
	}

	dashKey := "dash:" + c.ClientIP()
	if !s.hub.Security.CheckJoinRateLimit(dashKey) {
		c.Header("Retry-After", dashboardLoginRetryAfter)
		c.Data(http.StatusTooManyRequests, "text/html; charset=utf-8", []byte(loginFailedHTML))
		return
	}

	password := c.PostForm("password")
	if !s.auth.secretMatches(password) {
		s.hub.Security.RecordFailedJoin(dashKey)
		slog.Warn("dashboard login failed", "remote", c.ClientIP())
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusUnauthorized, loginFailedHTML)
		return
	}

	s.hub.Security.ClearFailedJoin(dashKey)

	expiresAt := time.Now().Add(s.auth.maxAge)
	cookie, err := s.auth.signCookie(expiresAt)
	if err != nil {
		slog.Error("Refusing to mint dashboard cookie", "error", err, "remote", c.ClientIP())
		c.Data(http.StatusInternalServerError, "text/html; charset=utf-8", []byte(loginInternalErrorHTML))
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(dashboardCookieName, cookie, int(s.auth.maxAge.Seconds()), "/dashboard", "", shouldUseSecureCookie(c.Request), true)
	c.Redirect(http.StatusFound, "/dashboard")
}

// handleDashboardLogout clears the cookie and returns to the login page.
// S4: the presented cookie is also added to the server-side revocation set
// so a stolen copy stops working immediately, not at the embedded expiry.
func (s *Server) handleDashboardLogout(c *gin.Context) {
	if !s.auth.enabled() {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if cookie, err := c.Cookie(dashboardCookieName); err == nil {
		s.auth.revoke(cookie)
		s.auth.purgeRevoked()
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(dashboardCookieName, "", -1, "/dashboard", "", shouldUseSecureCookie(c.Request), true)
	c.Redirect(http.StatusFound, "/dashboard/login")
}

// handleDashboardHistory returns the persisted time-series as JSON. Behind the
// same cookie auth as /dashboard. ?limit caps the tail size; default keeps a
// week of 5-min samples, hard-capped to bound response size.
func (s *Server) handleDashboardHistory(c *gin.Context) {
	if s.store == nil {
		c.JSON(http.StatusOK, []any{})
		return
	}
	limit := dashboardHistoryDefaultLimit
	if q := c.Query("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
			if limit > dashboardHistoryMaxLimit {
				limit = dashboardHistoryMaxLimit
			}
		}
	}
	samples, err := s.store.ReadSamples(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read failed"})
		return
	}
	c.JSON(http.StatusOK, samples)
}

func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}
