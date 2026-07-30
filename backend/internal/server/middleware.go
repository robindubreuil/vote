package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// requestIDHeader is the response header carrying the per-request ID so an
// operator can correlate a browser network entry with a server log line.
const requestIDHeader = "X-Request-ID"

// requestIDCtxKey is the gin-context key for the per-request ID. Hub/client
// code that wants to attach the ID to a log line reads it via
// RequestIDFromContext — but the WebSocket handler runs on a hijacked conn
// and no longer has the gin context, so client.go captures the ID into
// Client.RequestID at handshake time and uses it from there.
const requestIDCtxKey = "vote.request_id"

// requestIDBytes is the entropy budget for request IDs. 8 bytes (64 bits)
// base16-encodes to 16 chars — short enough to read in a log line, large
// enough that collisions over a 1000-req/s lifetime are vanishingly rare.
const requestIDBytes = 8

// generateRequestID returns a hex-encoded random ID. The empty-string
// fallback only fires if the system CSPRNG is unavailable, in which case
// the access log will simply omit the correlation field rather than fail
// the request.
func generateRequestID() string {
	b := make([]byte, requestIDBytes)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// requestIDMiddleware mints a per-request ID, exposes it on the gin context
// for downstream log enrichment, and echoes it back on the response so an
// operator can correlate a client-reported ID with a server log line. If
// the inbound request already carries an X-Request-ID (e.g. from an upstream
// proxy) and it is short enough to log safely, it is preserved.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" || len(id) > 64 {
			id = generateRequestID()
		}
		c.Set(requestIDCtxKey, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

// RequestIDFromContext returns the per-request ID stored by
// requestIDMiddleware, or "" if none is set (e.g. background work that
// didn't flow through an HTTP handler).
func RequestIDFromContext(c *gin.Context) string {
	v, ok := c.Get(requestIDCtxKey)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// accessLogMiddleware logs one structured line per HTTP request after the
// handler returns. The /health and /metrics endpoints are polled at fixed
// cadences (liveness probes, Prometheus scrapes, dashboard polling) and
// would drown the access log at INFO — they're logged at DEBUG instead,
// which the default JSON handler drops unless the level is lowered.
//
// B7: prior to this middleware the server had only gin.Recovery(); access
// lines and the request ID that correlates them with hub logs were both
// missing, so diagnosing a classroom flap required guessing which log
// line belonged to which client.
//
// S16: the resolved ClientIP is also fed to the server's loopback monitor
// so the watcher can warn when a reverse-proxy deploy left
// VOTE_TRUSTED_PROXIES unset. One IsLoopback check + two atomic adds per
// request — negligible on the hot path.
func (s *Server) accessLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		latency := time.Since(start)

		ip := c.ClientIP()
		if s.loopback != nil {
			s.loopback.observe(ip)
		}

		level := slog.LevelInfo
		if path == "/health" || path == "/metrics" {
			level = slog.LevelDebug
		}
		status := c.Writer.Status()
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		slog.LogAttrs(c.Request.Context(), level, "http request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Int("bytes", c.Writer.Size()),
			slog.Duration("latency", latency),
			slog.String("ip", ip),
			slog.String("request_id", RequestIDFromContext(c)),
		)
	}
}
