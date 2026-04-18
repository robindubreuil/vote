package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Timeouts(t *testing.T) {
	// Test defaults
	t.Run("defaults", func(t *testing.T) {
		// Ensure clean state
		os.Unsetenv("SESSION_TIMEOUT")
		os.Unsetenv("CLEANUP_INTERVAL")

		cfg := LoadConfig()

		if cfg.SessionTimeout != 1*time.Hour {
			t.Errorf("expected default session timeout 1h, got %v", cfg.SessionTimeout)
		}
		if cfg.CleanupInterval != 10*time.Minute {
			t.Errorf("expected default cleanup interval 10m, got %v", cfg.CleanupInterval)
		}
	})

	// Test environment overrides
	t.Run("env_overrides", func(t *testing.T) {
		os.Setenv("SESSION_TIMEOUT", "2h")
		os.Setenv("CLEANUP_INTERVAL", "30s")
		defer os.Unsetenv("SESSION_TIMEOUT")
		defer os.Unsetenv("CLEANUP_INTERVAL")

		cfg := LoadConfig()

		if cfg.SessionTimeout != 2*time.Hour {
			t.Errorf("expected session timeout 2h, got %v", cfg.SessionTimeout)
		}
		if cfg.CleanupInterval != 30*time.Second {
			t.Errorf("expected cleanup interval 30s, got %v", cfg.CleanupInterval)
		}
	})

	// Test invalid values (fallback to default)
	t.Run("invalid_values", func(t *testing.T) {
		os.Setenv("SESSION_TIMEOUT", "invalid")
		defer os.Unsetenv("SESSION_TIMEOUT")

		cfg := LoadConfig()

		if cfg.SessionTimeout != 1*time.Hour {
			t.Errorf("expected fallback to default 1h, got %v", cfg.SessionTimeout)
		}
	})
}
