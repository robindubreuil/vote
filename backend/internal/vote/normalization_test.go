package vote

import (
	"errors"
	"testing"
)

func TestNameNormalization(t *testing.T) {
	tests := []struct {
		name1       string
		name2       string
		shouldMatch bool
	}{
		{"Alice", "alice", true},
		{"BOB", "bob", true},
		{"Jean-Pierre", "jean pierre", true},
		{"Jean-Pierre", "jeanpierre", true},
		{" Marie ", "marie", true},
		{"User 1", "user1", true},
		{"Hélène", "Helene", true},
		{"François", "francois", true},
		{"Noël", "noel", true},
		{"Alice", "Bob", false},
	}

	for _, tt := range tests {
		n1 := NormalizeName(tt.name1)
		n2 := NormalizeName(tt.name2)
		if (n1 == n2) != tt.shouldMatch {
			t.Errorf("NormalizeName comparison failed for '%s' vs '%s': got %v, want %v (n1=%s, n2=%s)",
				tt.name1, tt.name2, n1 == n2, tt.shouldMatch, n1, n2)
		}
	}
}

// TestNormalizeNameCollisionsInSession verifies that name normalization
// is applied to stagiaire-collision checks (the CC2 authoritative check
// inside JoinStagiaire). Replaces the legacy TestGetStagiaireIDByName
// which depended on the now-removed name-based reclaim lookup — names
// are public, guessable, and ≤16 chars, so they can no longer be used
// to attach to an existing identity (S6).
func TestNormalizeNameCollisionsInSession(t *testing.T) {
	m := NewManager()
	_, _ = m.CreateSession("ABC", "trainer1")

	validID := "123456789012"
	if _, err := m.JoinStagiaire("ABC", validID, "Jean-Pierre", ""); err != nil {
		t.Fatalf("JoinStagiaire failed: %v", err)
	}

	// All normalized variants of "Jean-Pierre" collide with the existing
	// entry under a distinct ID — name-based reclaim is gone (S6), so a
	// second client presenting the same name cannot attach.
	for _, name := range []string{
		"jean-pierre",
		"Jean Pierre",
		"jeanpierre",
		"JEAN-PIERRE",
	} {
		if _, err := m.JoinStagiaire("ABC", "999999999999", name, ""); !errors.Is(err, ErrNameInUse) {
			t.Errorf("variant %q should collide with Jean-Pierre, got %v", name, err)
		}
	}

	// A non-matching name does not collide.
	if _, err := m.JoinStagiaire("ABC", "999999999998", "Jean-Paul", ""); err != nil {
		t.Errorf("Jean-Paul should not collide, got %v", err)
	}
}
