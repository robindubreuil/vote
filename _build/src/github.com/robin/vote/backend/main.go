package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// Server représente le serveur
type Server struct {
	hub *Hub
	r   *gin.Engine
}

func main() {
	// Configuration
	port := getEnv("PORT", "8080")
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "*")

	// Créer le hub
	hub := NewHub()
	go hub.Run()

	// Créer le serveur
	s := &Server{
		hub: hub,
		r:   gin.Default(),
	}

	// Configuration CORS
	s.setupCORS(allowedOrigins)

	// Routes
	s.r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	s.r.GET("/ws", s.HandleWebSocket)

	// Démarrer le serveur
	log.Printf("🗳️  Serveur Vote Coloré démarré sur http://localhost:%s", port)
	log.Printf("📡 WebSocket endpoint: ws://localhost:%s/ws", port)

	// Configuration du graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Serveur HTTP avec support pour le shutdown
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: s.r,
	}

	// Démarrer le serveur dans une goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Erreur démarrage serveur: %v", err)
		}
	}()

	// Attendre le signal d'arrêt
	<-ctx.Done()
	log.Println("Arrêt du serveur en cours...")

	// Arrêt avec timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Arrêter le hub
	s.hub.Shutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Erreur lors de l'arrêt du serveur: %v", err)
	}

	log.Println("Serveur arrêté proprement")
}

// setupCORS configure les headers CORS
func (s *Server) setupCORS(allowedOrigins string) {
	s.r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})
}

// getEnv récupère une variable d'environnement ou utilise une valeur par défaut
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
