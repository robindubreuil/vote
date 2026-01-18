package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"vote-backend/internal/config"
	"vote-backend/internal/hub"
)

type Server struct {
	router    *gin.Engine
	hub       *hub.Hub
	config    *config.Config
	srv       *http.Server
	startTime time.Time
}

func NewServer(cfg *config.Config, h *hub.Hub) *Server {
	r := gin.Default()
	s := &Server{
		router:    r,
		hub:       h,
		config:    cfg,
		startTime: time.Now(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.setupCORS()

	s.router.GET("/health", func(c *gin.Context) {
		metrics := s.hub.GetMetrics()
		c.JSON(200, gin.H{
			"status": "ok",
			"uptime_seconds": int64(time.Since(s.startTime).Seconds()),
			"metrics": metrics,
		})
	})

	s.router.GET("/ws", s.handleWebSocket)
}

func (s *Server) Run() error {
	s.srv = &http.Server{
		Addr:         ":" + s.config.Port,
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
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return s.config.IsOriginAllowed(origin)
		},
		MaxAge: 12 * time.Hour,
	}))
}

func (s *Server) handleWebSocket(c *gin.Context) {
	clientIP := getClientIP(c)

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

	client := hub.NewClient(s.hub, conn, clientIP)
	client.ID = s.hub.GenerateUniqueClientID() // Server-generated with collision detection

	client.Start()
}

func getClientIP(c *gin.Context) string {
	if forwarded := c.GetHeader("X-Forwarded-For"); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}
	if realIP := c.GetHeader("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	return c.ClientIP()
}
