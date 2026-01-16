package vote

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	MaxSessionCodeLength = 16
	MaxStagiaireIDLength = 64
	MaxNameLength        = 100

	sessionCodePattern = `^[a-zA-Z0-9-]{1,16}$`
	stagiaireIDPattern = `^[a-zA-Z0-9_-]{1,64}$`
)

var (
	sessionCodeRegex = regexp.MustCompile(sessionCodePattern)
	stagiaireIDRegex = regexp.MustCompile(stagiaireIDPattern)
)

func IsValidSessionCode(code string) bool {
	if code == "" || len(code) > MaxSessionCodeLength {
		return false
	}
	return sessionCodeRegex.MatchString(code)
}

func IsValidStagiaireID(id string) bool {
	if id == "" || len(id) > MaxStagiaireIDLength {
		return false
	}
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
