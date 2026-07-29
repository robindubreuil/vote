package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
	"vote-backend/internal/store"
	"vote-backend/internal/vote"
)

type Server struct {
	router      *gin.Engine
	hub         *hub.Hub
	config      *config.Config
	srv         *http.Server
	startTime   time.Time
	buildInfo   buildInfo
	auth        *dashboardAuth
	store       *store.Store
	statsMu     sync.Mutex
	statsCloser chan struct{}
	statsWG     sync.WaitGroup
}

func NewServer(cfg *config.Config, h *hub.Hub) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// B7: middleware order matters. Recovery wraps everything so panics
	// produce an access-log line + 500 instead of a dropped connection.
	// Request ID comes before the access log so the access line carries
	// the ID, and before the handlers so downstream code can read it.
	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(accessLogMiddleware())
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		slog.Warn("Failed to set trusted proxies", "error", err)
	}
	s := &Server{
		router:    r,
		hub:       h,
		config:    cfg,
		startTime: time.Now(),
		auth:      newDashboardAuth(cfg.DashboardSecret, cfg.DashboardMaxAge),
	}
	s.setupRoutes()
	return s
}

// EnablePersistence opens the on-disk store, restores the cumulative counters
// and histograms from the last checkpoint (so they read all-time across
// restarts), and starts a background goroutine that samples counters to disk.
// Returns an error if the store cannot be opened; in that case the server
// runs without persistence (counters reset on restart, as before).
func (s *Server) EnablePersistence() error {
	st, err := store.New(s.config.DataDir)
	if err != nil {
		return err
	}
	s.store = st
	base, err := st.LoadCounters()
	if err != nil {
		slog.Warn("Failed to load persisted counters, starting fresh", "error", err)
	} else if base.SessionsCreated > 0 || base.VotesStarted > 0 || base.VotesCast > 0 ||
		base.TraineesJoined > 0 || base.GameEnabledVotes > 0 || base.MultipleChoiceVotes > 0 ||
		base.SessionDuration.Count > 0 || base.VotesPerSession.Count > 0 || base.TraineesPerSession.Count > 0 {
		s.hub.VoteManager.Stats().Restore(vote.ProductStatsSnapshot{
			SessionsCreated:     base.SessionsCreated,
			VotesStarted:        base.VotesStarted,
			VotesCast:           base.VotesCast,
			TraineesJoined:      base.TraineesJoined,
			GameEnabledVotes:    base.GameEnabledVotes,
			MultipleChoiceVotes: base.MultipleChoiceVotes,
			SessionDuration:     toVoteHistogramSnapshot(base.SessionDuration),
			VotesPerSession:     toVoteHistogramSnapshot(base.VotesPerSession),
			TraineesPerSession:  toVoteHistogramSnapshot(base.TraineesPerSession),
		})
		slog.Info("Restored persisted counters",
			"sessions", base.SessionsCreated, "votes", base.VotesCast, "trainees", base.TraineesJoined,
			"histObs", base.SessionDuration.Count)
	}
	if err := st.Permissions(); err != nil {
		slog.Warn("Data dir permissions self-check", "error", err)
	}
	s.startStatsLoop()
	return nil
}

// startStatsLoop periodically flushes the current counters to disk: one
// append-only sample (for trends) and an atomic counters.json rewrite (for
// restore-on-boot). Worst-case crash loses at most one interval of increments.
func (s *Server) startStatsLoop() {
	if s.store == nil || s.config.StatsSampleInterval <= 0 {
		return
	}
	// One synchronous flush up front so a sample exists even if the process
	// exits before the first ticker fires. Done before launching the goroutine
	// so there is no race with a concurrent shutdown.
	s.flushStats()
	closer := make(chan struct{})
	s.statsMu.Lock()
	s.statsCloser = closer
	s.statsMu.Unlock()
	s.statsWG.Add(1)
	go func(done <-chan struct{}) {
		defer s.statsWG.Done()
		ticker := time.NewTicker(s.config.StatsSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.flushStats()
			case <-done:
				return
			}
		}
	}(closer)
}

func (s *Server) flushStats() {
	if s.store == nil {
		return
	}
	snap := s.hub.ProductStats()
	counters := store.Counters{
		Sample: store.Sample{
			Time:                time.Now(),
			SessionsCreated:     snap.SessionsCreated,
			VotesStarted:        snap.VotesStarted,
			VotesCast:           snap.VotesCast,
			TraineesJoined:      snap.TraineesJoined,
			GameEnabledVotes:    snap.GameEnabledVotes,
			MultipleChoiceVotes: snap.MultipleChoiceVotes,
		},
		SessionDuration:    toStoreHistogram(snap.SessionDuration),
		VotesPerSession:    toStoreHistogram(snap.VotesPerSession),
		TraineesPerSession: toStoreHistogram(snap.TraineesPerSession),
	}
	// The append-only log stores counters only — distributions don't need a
	// per-sample history and would bloat stats.jsonl by ~4x.
	if err := s.store.AppendSample(counters.Sample); err != nil {
		slog.Warn("Failed to append stats sample", "error", err)
	}
	if err := s.store.SaveCounters(counters); err != nil {
		slog.Warn("Failed to persist counters checkpoint", "error", err)
	}
}

// toStoreHistogram flattens a vote.HistogramSnapshot into the store-package
// shape. Kept here (not in store.go) so the store package stays free of any
// dependency on the vote package.
func toStoreHistogram(h vote.HistogramSnapshot) store.Histogram {
	buckets := make([]store.HistogramBucket, len(h.Buckets))
	for i, b := range h.Buckets {
		buckets[i] = store.HistogramBucket{LE: b.LE, Count: b.Count}
	}
	return store.Histogram{Count: h.Count, Sum: h.Sum, Buckets: buckets}
}

// toVoteHistogramSnapshot reverses toStoreHistogram at boot time so the vote
// package can replay the snapshot via ProductStats.Restore.
func toVoteHistogramSnapshot(h store.Histogram) vote.HistogramSnapshot {
	buckets := make([]vote.HistogramBucket, len(h.Buckets))
	for i, b := range h.Buckets {
		buckets[i] = vote.HistogramBucket{LE: b.LE, Count: b.Count}
	}
	return vote.HistogramSnapshot{Count: h.Count, Sum: h.Sum, Buckets: buckets}
}

// FlushStats stops the background sampler and writes one final checkpoint so
// the next boot restores to exactly here. Waits for the sampler goroutine to
// fully exit before returning, so the caller may safely CloseStore afterwards.
func (s *Server) FlushStats() {
	s.statsMu.Lock()
	closer := s.statsCloser
	s.statsCloser = nil
	s.statsMu.Unlock()
	if closer != nil {
		close(closer)
	}
	s.statsWG.Wait()
	s.flushStats()
}

// CloseStore releases the on-disk store handle.
func (s *Server) CloseStore() {
	if s.store != nil {
		s.store.Close()
	}
}

func (s *Server) setupRoutes() {
	s.setupCORS()

	// --- Health probes (D9) ---
	//
	// Two distinct probes following the Kubernetes convention, so an
	// orchestrator can tell "process is hung" (liveness) from "process
	// is up but should not receive traffic" (readiness):
	//
	//   /livez  — liveness. Returns 200 as long as the HTTP server is
	//             answering requests. Cheap by design: no locks, no
	//             stats reads. Use this for container HEALTHCHECK and
	//             liveness probes so a graceful drain does not get the
	//             process killed mid-shutdown.
	//
	//   /readyz — readiness. Returns 503 while the hub is draining
	//             (its context cancelled during shutdown); 200
	//             otherwise. Use this for load-balancer / readiness
	//             probes so traffic stops routing during drain.
	//
	// /health is kept as a backward-compatible alias of /readyz for the
	// CI wait loop, the e2e webServer probe, and any external monitors
	// already wired to it. It still returns the enriched payload
	// (uptime, metrics, persistence) those callers were built against.
	s.router.GET("/livez", s.handleLivez)
	s.router.GET("/readyz", s.handleReadyz)
	s.router.GET("/health", s.handleHealth)

	// D2: public build metadata. Mirrors the vote_build_info metric but
	// in a JSON shape that humans and non-Prometheus monitors can read
	// without parsing the exposition format. Public on purpose — the
	// version is already exposed via /metrics and helps operators
	// confirm which image a load-balanced node is serving.
	s.router.GET("/version", s.handleVersion)

	s.router.GET("/ws", s.handleWebSocket)
	s.router.GET("/metrics", s.handleMetrics)

	// Dashboard routes — registered only when VOTE_DASHBOARD_SECRET is set so
	// the routes do not exist at all when the feature is disabled.
	if s.auth.enabled() {
		dash := s.router.Group("/dashboard").Use(func(c *gin.Context) {
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; connect-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("X-Frame-Options", "DENY")
			c.Header("Referrer-Policy", "no-referrer")
			c.Next()
		})
		dash.GET("", s.requireAuth(), func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(dashboardHTML))
		})
		dash.GET("/login", s.handleDashboardLogin)
		dash.POST("/login", s.handleDashboardLogin)
		dash.POST("/logout", s.handleDashboardLogout)
		dash.GET("/history", s.requireAuth(), s.handleDashboardHistory)
	}
}

func (s *Server) Run() error {
	s.srv = &http.Server{
		Addr:         net.JoinHostPort(s.config.Host, s.config.Port),
		Handler:      s.router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	slog.Info("Server starting", "port", s.config.Port)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) setupCORS() {
	s.router.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization", "accept", "origin", "Cache-Control", "X-Requested-With"},
		AllowCredentials: s.config.AllowCredentials,
		AllowOriginFunc: func(origin string) bool {
			return s.config.IsOriginAllowed(origin)
		},
		MaxAge: 12 * time.Hour,
	}))
}

func (s *Server) handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":    s.buildInfo.versionOrDefault(),
		"build_time": s.buildInfo.buildTimeOrDefault(),
		"git_commit": s.buildInfo.gitCommitOrDefault(),
	})
}

// handleLivez is the liveness probe: the fact that the request reached a
// handler means the HTTP server goroutine is scheduling, so we return
// 200 with a tiny payload. Intentionally cheap — no locks, no stats
// reads — so a deadlocked hot path still trips the probe.
func (s *Server) handleLivez(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":         "alive",
		"uptime_seconds": int64(time.Since(s.startTime).Seconds()),
	})
}

// handleReadyz is the readiness probe. The hub's context is cancelled
// during Shutdown, so a non-nil Err() means we are draining and must
// stop receiving traffic. Otherwise we are ready.
func (s *Server) handleReadyz(c *gin.Context) {
	if s.hub.Context().Err() != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "draining"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":         "ready",
		"uptime_seconds": int64(time.Since(s.startTime).Seconds()),
		"persistence":    s.store != nil,
	})
}

// handleHealth is the backward-compatible alias of /readyz, retaining
// the enriched payload (metrics + persistence + uptime) that existing
// monitors and CI wait-loops were built against.
func (s *Server) handleHealth(c *gin.Context) {
	if s.hub.Context().Err() != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "draining"})
		return
	}
	metrics := s.hub.GetMetrics()
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"uptime_seconds": int64(time.Since(s.startTime).Seconds()),
		"metrics":        metrics,
		"persistence":    s.store != nil,
	})
}

func (s *Server) handleWebSocket(c *gin.Context) {
	clientIP := c.ClientIP()

	allowed := s.hub.Security.CheckJoinRateLimit(clientIP)
	if !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Too many attempts, please try again later",
		})
		return
	}

	// S7: per-IP concurrent-connection cap. Acquired BEFORE the WS
	// upgrade so a rejected dial never consumes a file descriptor or
	// goroutine. The slot is released by Hub.unregisterClient when the
	// client's readPump exits (every readPump defer routes through
	// Unregister, even if the client never sent a join message).
	if !s.hub.AcquireIPSlot(clientIP) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Too many concurrent connections from this network",
		})
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			// Browsers always send Origin on cross-origin WebSocket
			// handshakes. Absence signals a non-browser client, which is
			// exactly the profile of a scripted takeover or smuggling
			// attempt. Reject it rather than implicitly trusting.
			if origin == "" {
				return false
			}
			return s.config.IsOriginAllowed(origin)
		},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("WebSocket upgrade error",
			"ip", clientIP, "request_id", RequestIDFromContext(c), "error", err)
		// Upgrade failed — release the slot we reserved so the cap
		// doesn't slowly drain on handshake errors.
		s.hub.ReleaseIPSlot(clientIP)
		return
	}

	clientID, ok := s.hub.GenerateUniqueClientID()
	if !ok {
		slog.Error("Failed to generate unique client ID",
			"ip", clientIP, "request_id", RequestIDFromContext(c))
		conn.Close()
		// readPump never started, so unregisterClient never runs —
		// release manually.
		s.hub.ReleaseIPSlot(clientIP)
		return
	}

	client := hub.NewClient(s.hub, conn, clientIP)
	client.ID = clientID
	// B7: propagate the per-request ID so hub log lines can be
	// correlated with the HTTP access line that recorded the WS
	// upgrade. Read from the gin context set by requestIDMiddleware;
	// falls back to "" if the context value is absent.
	client.RequestID = RequestIDFromContext(c)

	client.Start()
}
