package vote

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	MaxNameLength  = 16
	MaxLabelLength = 6   // Matches frontend maxlength for UI brevity

	// Server-generated identifiers
	sessionCodePattern = `^\d{4}$`        // 4-digit codes
	stagiaireIDPattern = `^[a-z0-9]{12}$` // 12-char crypto-random
)

var (
	sessionCodeRegex = regexp.MustCompile(sessionCodePattern)
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
	if len(name) == 0 || len(name) > MaxNameLength {
		return false
	}
	name = strings.TrimSpace(name)
	if len(name) == 0 {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != ' ' && r != '-' && r != '\'' {
			return false
		}
	}
	return true
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
	if len(label) == 0 || len(label) > MaxLabelLength {
		return false
	}
	// Allow letters, digits, spaces, hyphens, apostrophes
	for _, r := range label {
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
