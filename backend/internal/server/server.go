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
	r.Use(gin.Recovery())
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
// from the last checkpoint (so they read all-time across restarts), and starts
// a background goroutine that samples counters to disk. Returns an error if the
// store cannot be opened; in that case the server runs without persistence
// (counters reset on restart, as before).
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
		base.TraineesJoined > 0 || base.GameEnabledVotes > 0 || base.MultipleChoiceVotes > 0 {
		s.hub.VoteManager.Stats().Restore(vote.ProductStatsSnapshot{
			SessionsCreated:     base.SessionsCreated,
			VotesStarted:        base.VotesStarted,
			VotesCast:           base.VotesCast,
			TraineesJoined:      base.TraineesJoined,
			GameEnabledVotes:    base.GameEnabledVotes,
			MultipleChoiceVotes: base.MultipleChoiceVotes,
		})
		slog.Info("Restored persisted counters",
			"sessions", base.SessionsCreated, "votes", base.VotesCast, "trainees", base.TraineesJoined)
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
	sample := store.Sample{
		Time:                time.Now(),
		SessionsCreated:     snap.SessionsCreated,
		VotesStarted:        snap.VotesStarted,
		VotesCast:           snap.VotesCast,
		TraineesJoined:      snap.TraineesJoined,
		GameEnabledVotes:    snap.GameEnabledVotes,
		MultipleChoiceVotes: snap.MultipleChoiceVotes,
	}
	if err := s.store.AppendSample(sample); err != nil {
		slog.Warn("Failed to append stats sample", "error", err)
	}
	if err := s.store.SaveCounters(sample); err != nil {
		slog.Warn("Failed to persist counters checkpoint", "error", err)
	}
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

	s.router.GET("/health", func(c *gin.Context) {
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
	})

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

func (s *Server) handleWebSocket(c *gin.Context) {
	clientIP := c.ClientIP()

	allowed := s.hub.Security.CheckJoinRateLimit(clientIP)
	if !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Too many attempts, please try again later",
		})
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return s.config.IsOriginAllowed(origin)
		},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("WebSocket upgrade error", "error", err)
		return
	}

	clientID, ok := s.hub.GenerateUniqueClientID()
	if !ok {
		slog.Error("Failed to generate unique client ID")
		conn.Close()
		return
	}

	client := hub.NewClient(s.hub, conn, clientIP)
	client.ID = clientID

	client.Start()
}
