package vote

import (
	"errors"
	"testing"
)

// TestUserFacingErrorMapping is the B3 regression table: every error a
// manager method can return must map to a French string at the client
// boundary (or to a safe French fallback for internal sentinels). This
// guards against a future leaf error accidentally leaking internal wording
// into the classroom UI.
func TestUserFacingErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil is empty", nil, ""},
		{"session not found", ErrSessionNotFound, "Session introuvable"},
		{"unauthorized", ErrUnauthorized, "Action non autorisée"},
		{"invalid input", ErrInvalidInput, "Saisie invalide"},
		{"name in use", ErrNameInUse, "Ce nom est déjà utilisé"},
		{"reclaim unauthorized", ErrReclaimUnauthorized, "Session expirée — veuillez recréer votre identité"},
		{"vote already active", ErrVoteAlreadyActive, ErrVoteAlreadyActive.Error()},
		{"vote not active", ErrVoteNotActive, ErrVoteNotActive.Error()},
		{"single choice only", ErrSingleChoiceOnly, ErrSingleChoiceOnly.Error()},
		{"duplicate colors", ErrDuplicateColors, ErrDuplicateColors.Error()},
		{"blank not allowed", ErrBlankNotAllowed, ErrBlankNotAllowed.Error()},
		{"blank with colors", ErrBlankWithColors, ErrBlankWithColors.Error()},
		{"at least one color", ErrAtLeastOneColor, ErrAtLeastOneColor.Error()},
		{"invalid color", ErrInvalidColor, ErrInvalidColor.Error()},
		{"vote not closed", ErrVoteNotClosed, ErrVoteNotClosed.Error()},
		{"stagiaire not found", ErrStagiaireNotFound, ErrStagiaireNotFound.Error()},
		{"not authorized", ErrNotAuthorized, ErrNotAuthorized.Error()},
		{"unknown fallback", errors.New("internal boom"), "Une erreur est survenue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UserFacingError(tc.err)
			if got != tc.want {
				t.Errorf("UserFacingError(%v): got %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestUserFacingErrorRejectsEnglishSentinels asserts that the wire stays
// French: the previously-leaked English strings must never reach a client
// again. If any of these literal substrings appear in the mapped message,
// the regression has returned.
func TestUserFacingErrorRejectsEnglishSentinels(t *testing.T) {
	englishLeaves := []string{
		"unauthorized",
		"vote is not active",
		"only one color allowed",
		"stagiaire not found",
		"duplicate colors are not allowed",
		"blank votes are not allowed",
		"at least one color is required",
		"invalid color",
		"vote must be closed",
	}
	checked := []error{
		ErrSessionNotFound, ErrUnauthorized, ErrInvalidInput,
		ErrVoteNotActive, ErrSingleChoiceOnly, ErrDuplicateColors,
		ErrBlankNotAllowed, ErrBlankWithColors, ErrAtLeastOneColor,
		ErrInvalidColor, ErrVoteNotClosed, ErrStagiaireNotFound,
		ErrNotAuthorized, ErrVoteAlreadyActive, ErrNameInUse, ErrReclaimUnauthorized,
	}
	for _, err := range checked {
		msg := UserFacingError(err)
		for _, leaf := range englishLeaves {
			if containsLower(msg, leaf) {
				t.Errorf("client-facing message %q leaks English substring %q (B3 regression)", msg, leaf)
			}
		}
	}
}

// containsLower case-insensitive substring check.
func containsLower(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			c1 := haystack[i+j]
			c2 := needle[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
