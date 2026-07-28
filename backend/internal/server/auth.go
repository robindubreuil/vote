package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	dashboardCookieName = "vote_admin"
	dashboardCookieVal  = "v1"
	// dashboardNonceBytes makes each minted cookie unique even when the
	// expiry lands in the same second. Without it, signCookie is
	// deterministic over (secret, expiry) and two logins within one
	// second produce identical tokens — so revoking one would revoke
	// both, and the S4 revocation test can't distinguish them.
	dashboardNonceBytes = 16
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

// signCookie builds an HMAC-SHA256 over the payload (version + nonce +
// expiry) and returns "payload.signature" both base64url-encoded. The nonce
// makes each minting unique; the signature binds the expiry so a leaked
// cookie cannot have its lifetime extended without the secret.
func (a *dashboardAuth) signCookie(expiresAt time.Time) string {
	nonce := make([]byte, dashboardNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		slog.Error("Failed to generate dashboard cookie nonce", "error", err)
	}
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	payload := dashboardCookieVal + "." + hex.EncodeToString(nonce) + "." + exp
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
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
func shouldUseSecureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if isLoopbackRemoteAddr(r.RemoteAddr) {
		return false
	}
	return true
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
		c.Header("Retry-After", "60")
		c.Data(http.StatusTooManyRequests, "text/html; charset=utf-8", []byte(loginFailedHTML))
		return
	}

	password := c.PostForm("password")
	if subtle.ConstantTimeCompare([]byte(password), s.auth.secret) != 1 {
		s.hub.Security.RecordFailedJoin(dashKey)
		slog.Warn("dashboard login failed", "remote", c.ClientIP())
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusUnauthorized, loginFailedHTML)
		return
	}

	s.hub.Security.ClearFailedJoin(dashKey)

	expiresAt := time.Now().Add(s.auth.maxAge)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(dashboardCookieName, s.auth.signCookie(expiresAt), int(s.auth.maxAge.Seconds()), "/dashboard", "", shouldUseSecureCookie(c.Request), true)
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
	limit := 2016 // 7 days * 288 samples/day at 5-min cadence
	const maxLimit = 20000
	if q := c.Query("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
			if limit > maxLimit {
				limit = maxLimit
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
