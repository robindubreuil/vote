package vote

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxNameLength  = 16
	MaxLabelLength = 12 // Matches frontend maxlength for UI brevity

	// Session codes are uppercase letters drawn from a disambiguation-safe
	// alphabet (no I/O/Z, which are easily confused with 1/0/2 on a phone
	// screen or read aloud in a classroom).
	//
	// S15: codes are 4 characters. The old 3-char space (23^3 = 12,167
	// codes) was enumerable within the join-rate backoff budget (~20
	// minutes of scripted guessing); 4 chars multiplies the space by 23
	// (≈280k) at negligible typing cost. Validation still accepts the
	// legacy 3-char length so pre-upgrade sessions and pinned test
	// fixtures keep working — all freshly generated codes are 4 chars,
	// which is what matters for enumeration resistance.
	SessionCodeLength = 4
	SessionAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXY"

	// Server-generated identifiers
	stagiaireIDPattern = `^[a-z0-9]{12}$` // 12-char crypto-random
)

var (
	// sessionCodeRegex strictly enforces membership in SessionAlphabet so that
	// typos involving excluded letters (I, O, Z) are rejected with a clear
	// "invalid code" rather than producing a "session not found" downstream.
	// {3,4}: 4 is the generation length (S15); 3 keeps legacy codes valid.
	sessionCodeRegex = regexp.MustCompile(`^[ABCDEFGHJKLMNPQRSTUVWXY]{3,4}$`)
	stagiaireIDRegex = regexp.MustCompile(stagiaireIDPattern)
)

func IsValidSessionCode(code string) bool {
	if code == "" {
		return false
	}
	return sessionCodeRegex.MatchString(code)
}

func IsValidStagiaireID(id string) bool {
	return stagiaireIDRegex.MatchString(id)
}

func IsValidName(name string) bool {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > MaxNameLength {
		return false
	}
	return hasValidCharacters(name)
}

// ValidateColors checks if all colors in the slice are present in the allowed list
func ValidateColors(colors []string, allowed []string) bool {
	allowedMap := make(map[string]bool)
	for _, c := range allowed {
		allowedMap[c] = true
	}

	for _, c := range colors {
		if !allowedMap[c] {
			return false
		}
	}
	return true
}

// IsValidLabel validates custom color labels
func IsValidLabel(label string) bool {
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) == 0 || utf8.RuneCountInString(label) > MaxLabelLength {
		return false
	}
	return hasValidCharacters(label)
}

func hasValidCharacters(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != ' ' && r != '-' && r != '\'' {
			return false
		}
	}
	return true
}

// ValidateLabels validates the labels map
// Returns true if all keys are valid colors and all values are valid labels
func ValidateLabels(labels map[string]string, allowedColors []string) bool {
	allowedMap := make(map[string]bool)
	for _, c := range allowedColors {
		allowedMap[c] = true
	}

	for colorID, label := range labels {
		if !allowedMap[colorID] {
			return false
		}
		if !IsValidLabel(label) {
			return false
		}
	}
	return true
}

func HasDuplicates(items []string) bool {
	seen := make(map[string]bool)
	for _, item := range items {
		if seen[item] {
			return true
		}
		seen[item] = true
	}
	return false
}
