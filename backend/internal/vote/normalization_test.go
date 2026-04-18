package vote

import (
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
		n1 := normalizeName(tt.name1)
		n2 := normalizeName(tt.name2)
		if (n1 == n2) != tt.shouldMatch {
			t.Errorf("normalizeName comparison failed for '%s' vs '%s': got %v, want %v (n1=%s, n2=%s)",
				tt.name1, tt.name2, n1 == n2, tt.shouldMatch, n1, n2)
		}
	}
}

func TestGetStagiaireIDByName(t *testing.T) {
	m := NewManager()
	_, _ = m.CreateSession("1234", "trainer1")

	validID := "123456789012"
	// Add initial user
	err := m.JoinStagiaire("1234", validID, "Jean-Pierre")
	if err != nil {
		t.Fatalf("JoinStagiaire failed: %v", err)
	}

	// Test finding with different variations
	variations := []string{
		"jean-pierre",
		"Jean Pierre",
		"jeanpierre",
		"JEAN-PIERRE",
	}

	for _, name := range variations {
		id, found := m.GetStagiaireIDByName("1234", name)
		if !found {
			t.Errorf("Failed to find existing user 'Jean-Pierre' using variation '%s'", name)
		}
		if id != validID {
			t.Errorf("Found wrong ID for variation '%s': got %s, want %s", name, id, validID)
		}
	}

	// Test non-match
	_, found := m.GetStagiaireIDByName("1234", "Jean-Paul")
	if found {
		t.Error("Should not find non-existent user 'Jean-Paul'")
	}
}
