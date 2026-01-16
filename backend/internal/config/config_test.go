package config

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	os.Setenv("PORT", "9090")
	defer os.Unsetenv("PORT")

	cfg := LoadConfig()
	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %s", cfg.Port)
	}
}

func TestIsOriginAllowed(t *testing.T) {
	cfg := &Config{
		AllowedOrigins: []string{"http://localhost:3000"},
	}

	if !cfg.IsOriginAllowed("http://localhost:3000") {
		t.Error("should allow exact match")
	}
	if cfg.IsOriginAllowed("http://evil.com") {
		t.Error("should deny unknown origin")
	}

	cfg.AllowedOrigins = []string{"*"}
	if !cfg.IsOriginAllowed("http://google.com") {
		t.Error("wildcard should allow all")
	}
}
