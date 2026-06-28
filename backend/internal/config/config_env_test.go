package config

import (
	"testing"
	"time"
)

func TestLoadConfig_Timeouts(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("SESSION_TIMEOUT", "")
		t.Setenv("CLEANUP_INTERVAL", "")

		cfg := LoadConfig()

		if cfg.SessionTimeout != 1*time.Hour {
			t.Errorf("expected default session timeout 1h, got %v", cfg.SessionTimeout)
		}
		if cfg.CleanupInterval != 10*time.Minute {
			t.Errorf("expected default cleanup interval 10m, got %v", cfg.CleanupInterval)
		}
	})

	t.Run("env_overrides", func(t *testing.T) {
		t.Setenv("SESSION_TIMEOUT", "2h")
		t.Setenv("CLEANUP_INTERVAL", "30s")

		cfg := LoadConfig()

		if cfg.SessionTimeout != 2*time.Hour {
			t.Errorf("expected session timeout 2h, got %v", cfg.SessionTimeout)
		}
		if cfg.CleanupInterval != 30*time.Second {
			t.Errorf("expected cleanup interval 30s, got %v", cfg.CleanupInterval)
		}
	})

	t.Run("invalid_values", func(t *testing.T) {
		t.Setenv("SESSION_TIMEOUT", "invalid")

		cfg := LoadConfig()

		if cfg.SessionTimeout != 1*time.Hour {
			t.Errorf("expected fallback to default 1h, got %v", cfg.SessionTimeout)
		}
	})
}

func TestWildcardOriginDisablesCredentials(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "*")

	cfg := LoadConfig()

	if cfg.AllowCredentials {
		t.Error("wildcard origin should disable credentials")
	}
	if len(cfg.AllowedOrigins) != 0 {
		t.Error("wildcard should result in empty origins list")
	}
}

func TestSpecificOriginsEnableCredentials(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:5173")

	cfg := LoadConfig()

	if !cfg.AllowCredentials {
		t.Error("specific origins should enable credentials")
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "http://localhost:5173" {
		t.Errorf("unexpected origins: %v", cfg.AllowedOrigins)
	}
}
