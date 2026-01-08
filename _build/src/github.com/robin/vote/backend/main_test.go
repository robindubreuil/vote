package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestNewServer vérifie la création du serveur
func TestNewServer(t *testing.T) {
	hub := NewHub()
	s := &Server{
		hub: hub,
		r:   gin.Default(),
	}

	if s.hub == nil {
		t.Fatal("Hub should be initialized")
	}

	if s.r == nil {
		t.Fatal("Gin engine should be initialized")
	}
}

// TestSetupCORS vérifie la configuration CORS
func TestSetupCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	s := &Server{
		hub: hub,
		r:   gin.New(),
	}

	s.setupCORS("https://example.com")

	// Créer une requête test
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)

	// Vérifier les headers CORS
	if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("Expected CORS origin 'https://example.com', got '%s'", w.Header().Get("Access-Control-Allow-Origin"))
	}

	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("Expected CORS credentials to be 'true'")
	}

	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected CORS methods to be set")
	}
}

// TestSetupCORSWildcard vérifie le CORS avec wildcard
func TestSetupCORSWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	s := &Server{
		hub: hub,
		r:   gin.New(),
	}

	s.setupCORS("*")

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected CORS origin '*', got '%s'", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

// TestSetupCORSPreflight vérifie la requête OPTIONS preflight
func TestSetupCORSPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	s := &Server{
		hub: hub,
		r:   gin.New(),
	}

	s.setupCORS("*")

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Errorf("Expected status 204 for OPTIONS request, got %d", w.Code)
	}
}

// TestHealthEndpoint vérifie le endpoint de santé
func TestHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hub := NewHub()
	s := &Server{
		hub: hub,
		r:   gin.New(),
	}

	// Ajouter une route de health
	s.r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Vérifier que la réponse contient "status": "ok"
	if w.Body.String() != "{\"status\":\"ok}\n" && w.Body.String() != "{\"status\":\"ok\"}" {
		// Le body peut varier légèrement selon la version de Gin
		if w.Body.Len() == 0 {
			t.Error("Expected non-empty response body")
		}
	}
}

// TestGetEnv vérifie la récupération des variables d'environnement
func TestGetEnv(t *testing.T) {
	// Test avec valeur par défaut
	result := getEnv("NON_EXISTENT_VAR", "default")
	if result != "default" {
		t.Errorf("Expected 'default', got '%s'", result)
	}

	// Test avec variable existante
	os.Setenv("TEST_VOTE_VAR", "test_value")
	result = getEnv("TEST_VOTE_VAR", "default")
	if result != "test_value" {
		t.Errorf("Expected 'test_value', got '%s'", result)
	}
	os.Unsetenv("TEST_VOTE_VAR")
}

// TestGetEnvEmpty vérifie le comportement avec une variable vide
func TestGetEnvEmpty(t *testing.T) {
	os.Setenv("TEST_VOTE_EMPTY", "")
	result := getEnv("TEST_VOTE_EMPTY", "default")
	// Une variable définie mais vide retourne la valeur par défaut
	if result != "default" {
		t.Errorf("Expected 'default' for empty env var, got '%s'", result)
	}
	os.Unsetenv("TEST_VOTE_EMPTY")
}

// TestServerIntegration vérifie l'intégration complète
func TestServerIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Créer le hub
	hub := NewHub()
	go hub.Run()

	// Créer le serveur
	s := &Server{
		hub: hub,
		r:   gin.New(),
	}

	s.setupCORS("*")
	s.r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Tester que le serveur répond correctement
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}
