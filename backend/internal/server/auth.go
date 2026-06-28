package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	dashboardCookieName = "vote_admin"
	dashboardCookieVal  = "v1"
)

// dashboardAuth holds the configuration for the cookie auth scheme. A zero
// value (empty secret) means the dashboard is disabled.
type dashboardAuth struct {
	secret []byte
	maxAge time.Duration
}

func newDashboardAuth(secret string, maxAge time.Duration) *dashboardAuth {
	if secret == "" {
		return nil
	}
	return &dashboardAuth{secret: []byte(secret), maxAge: maxAge}
}

func (a *dashboardAuth) enabled() bool { return a != nil }

// signCookie builds an HMAC-SHA256 over the payload (version + expiry) and
// returns "payload.signature" both base64url-encoded. The signature binds the
// expiry so a leaked cookie cannot have its lifetime extended without the
// secret.
func (a *dashboardAuth) signCookie(expiresAt time.Time) string {
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	payload := dashboardCookieVal + "." + exp
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// verifyCookie validates both the signature and the expiry. Returns true only
// when the signature matches (constant-time) AND the cookie has not expired.
func (a *dashboardAuth) verifyCookie(raw string) bool {
	if raw == "" {
		return false
	}
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) != 3 {
		return false
	}
	payload := parts[0] + "." + parts[1]
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
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
	expUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Before(time.Unix(expUnix, 0))
}

// shouldUseSecureCookie returns true when the connection is encrypted (TLS) or
// is not loopback. On plain-HTTP loopback (local dev) Secure is relaxed so the
// browser actually persists the cookie; production behind TLS always sets it.
func shouldUseSecureCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false
	}
	return true
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
func (s *Server) handleDashboardLogout(c *gin.Context) {
	if !s.auth.enabled() {
		c.AbortWithStatus(http.StatusNotFound)
		return
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
