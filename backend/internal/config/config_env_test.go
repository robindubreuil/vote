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

// TestResourceCapsDefaultsAndOverrides covers S7: the three resource
// caps have documented defaults and are overridable via env. A zero or
// negative value disables each cap independently — used by tests.
func TestResourceCapsDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("VOTE_MAX_SESSIONS", "")
		t.Setenv("VOTE_MAX_CLIENTS_PER_SESSION", "")
		t.Setenv("VOTE_MAX_CONNECTIONS_PER_IP", "")

		cfg := LoadConfig()

		if cfg.MaxSessionsGlobal != 1000 {
			t.Errorf("MaxSessionsGlobal default: got %d, want 1000", cfg.MaxSessionsGlobal)
		}
		if cfg.MaxClientsPerSession != 200 {
			t.Errorf("MaxClientsPerSession default: got %d, want 200", cfg.MaxClientsPerSession)
		}
		if cfg.MaxConnectionsPerIP != 50 {
			t.Errorf("MaxConnectionsPerIP default: got %d, want 50", cfg.MaxConnectionsPerIP)
		}
	})

	t.Run("env_overrides", func(t *testing.T) {
		t.Setenv("VOTE_MAX_SESSIONS", "5")
		t.Setenv("VOTE_MAX_CLIENTS_PER_SESSION", "3")
		t.Setenv("VOTE_MAX_CONNECTIONS_PER_IP", "7")

		cfg := LoadConfig()

		if cfg.MaxSessionsGlobal != 5 {
			t.Errorf("MaxSessionsGlobal override: got %d, want 5", cfg.MaxSessionsGlobal)
		}
		if cfg.MaxClientsPerSession != 3 {
			t.Errorf("MaxClientsPerSession override: got %d, want 3", cfg.MaxClientsPerSession)
		}
		if cfg.MaxConnectionsPerIP != 7 {
			t.Errorf("MaxConnectionsPerIP override: got %d, want 7", cfg.MaxConnectionsPerIP)
		}
	})

	t.Run("disable_via_zero", func(t *testing.T) {
		t.Setenv("VOTE_MAX_SESSIONS", "0")
		t.Setenv("VOTE_MAX_CLIENTS_PER_SESSION", "-1")

		cfg := LoadConfig()

		if cfg.MaxSessionsGlobal != 0 {
			t.Errorf("MaxSessionsGlobal should be 0 (disabled), got %d", cfg.MaxSessionsGlobal)
		}
		if cfg.MaxClientsPerSession != -1 {
			t.Errorf("MaxClientsPerSession should be -1 (disabled), got %d", cfg.MaxClientsPerSession)
		}
	})
}
