package main

import (
	"log"
	"os"

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

	if err := s.r.Run(":" + port); err != nil {
		log.Fatalf("Erreur démarrage serveur: %v", err)
	}
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
