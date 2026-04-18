package vote

import (
	"testing"
)

func TestIsValidSessionCode(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"1234", true},
		{"0000", true},
		{"9999", true},
		{"", false},
		{"123", false},
		{"12345", false},
		{"abcd", false},
		{"12 4", false},
		{"12.4", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := IsValidSessionCode(tt.code); got != tt.want {
				t.Errorf("IsValidSessionCode(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestIsValidStagiaireID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"abc123def456", true},
		{"abcdefghij12", true},
		{"123456789012", true},
		{"", false},
		{"abc", false},
		{"ABC123def456", false},
		{"abc123def4567", false},
		{"abc123def45!", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := IsValidStagiaireID(tt.id); got != tt.want {
				t.Errorf("IsValidStagiaireID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestIsValidName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Marie", true},
		{"Jean-Pierre", true},
		{"O'Brien", true},
		{"Anne Marie", true},
		{"émilie", true},
		{"", false},
		{"   ", false},
		{"a", true},
		{"<script>", false},
		{"Test!", false},
		{"Test@Mail", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidName(tt.name); got != tt.want {
				t.Errorf("IsValidName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsValidNameLength(t *testing.T) {
	name16 := "aaaaaaaaaaaaaaaa"
	if len(name16) != 16 {
		t.Fatal("test setup error")
	}
	if !IsValidName(name16) {
		t.Error("16-char name should be valid")
	}

	longName := "aaaaaaaaaaaaaaaaa"
	if IsValidName(longName) {
		t.Error("17-char name should be invalid")
	}

	accented := "éééééééééééééééé"
	if len([]rune(accented)) != 16 {
		t.Fatal("test setup error: expected 16 runes")
	}
	if !IsValidName(accented) {
		t.Error("16-rune accented name should be valid")
	}

	accented17 := "ééééééééééééééééé"
	if len([]rune(accented17)) != 17 {
		t.Fatal("test setup error: expected 17 runes")
	}
	if IsValidName(accented17) {
		t.Error("17-rune accented name should be invalid")
	}
}

func TestValidateColors(t *testing.T) {
	allowed := []string{"rouge", "vert", "bleu"}

	if !ValidateColors([]string{"rouge"}, allowed) {
		t.Error("single valid color should pass")
	}
	if !ValidateColors([]string{"rouge", "vert"}, allowed) {
		t.Error("multiple valid colors should pass")
	}
	if !ValidateColors([]string{}, allowed) {
		t.Error("empty colors should pass")
	}
	if ValidateColors([]string{"jaune"}, allowed) {
		t.Error("invalid color should fail")
	}
	if ValidateColors([]string{"rouge", "jaune"}, allowed) {
		t.Error("mix of valid and invalid should fail")
	}
}

func TestHasDuplicates(t *testing.T) {
	if HasDuplicates([]string{"a", "b", "c"}) {
		t.Error("unique items should not have duplicates")
	}
	if !HasDuplicates([]string{"a", "b", "a"}) {
		t.Error("duplicate items should be detected")
	}
	if HasDuplicates([]string{}) {
		t.Error("empty slice should not have duplicates")
	}
	if HasDuplicates([]string{"a"}) {
		t.Error("single item should not have duplicates")
	}
}

func TestIsValidLabel(t *testing.T) {
	if !IsValidLabel("Rouge") {
		t.Error("valid label should pass")
	}
	if !IsValidLabel("abc123") {
		t.Error("alphanumeric label should pass")
	}
	if IsValidLabel("") {
		t.Error("empty label should fail")
	}
	if IsValidLabel("   ") {
		t.Error("whitespace-only label should fail")
	}
	if IsValidLabel("1234567") {
		t.Error("7-char label should fail (max 6)")
	}
	if IsValidLabel("test!") {
		t.Error("label with special char should fail")
	}
}

func TestValidateLabels(t *testing.T) {
	allowed := []string{"rouge", "vert", "bleu"}

	if !ValidateLabels(map[string]string{"rouge": "R"}, allowed) {
		t.Error("valid label should pass")
	}
	if !ValidateLabels(map[string]string{}, allowed) {
		t.Error("empty labels should pass")
	}
	if ValidateLabels(map[string]string{"jaune": "R"}, allowed) {
		t.Error("invalid color key should fail")
	}
	if ValidateLabels(map[string]string{"rouge": "test!"}, allowed) {
		t.Error("invalid label value should fail")
	}
}
