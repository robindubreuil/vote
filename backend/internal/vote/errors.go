package vote

import "errors"

// UserFacingError maps a manager-level error to a French string suitable for
// sending verbatim to a WebSocket client. Sentinel errors defined in this
// package already carry French messages (B3); they pass through unchanged.
// Internal English sentinels (ErrSessionNotFound, ErrUnauthorized, ErrInvalidInput)
// that must not be exposed to clients are mapped to safe French fallbacks.
// Unknown errors collapse to a generic message so a future leaf error can't
// accidentally leak internal wording into a classroom UI.
//
// Callers should use this at every SendError boundary that forwards
// manager-generated errors, e.g. the hub's per-message handlers.
func UserFacingError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrSessionNotFound):
		return "Session introuvable"
	case errors.Is(err, ErrUnauthorized):
		return "Action non autorisée"
	case errors.Is(err, ErrInvalidInput):
		return "Saisie invalide"
	case errors.Is(err, ErrNameInUse):
		return "Ce nom est déjà utilisé"
	case errors.Is(err, ErrReclaimUnauthorized):
		return "Session expirée — veuillez recréer votre identité"
	case errors.Is(err, ErrVoteAlreadyActive),
		errors.Is(err, ErrVoteNotActive),
		errors.Is(err, ErrSingleChoiceOnly),
		errors.Is(err, ErrDuplicateColors),
		errors.Is(err, ErrBlankNotAllowed),
		errors.Is(err, ErrBlankWithColors),
		errors.Is(err, ErrAtLeastOneColor),
		errors.Is(err, ErrInvalidColor),
		errors.Is(err, ErrVoteNotClosed),
		errors.Is(err, ErrStagiaireNotFound),
		errors.Is(err, ErrNotAuthorized),
		errors.Is(err, ErrGameDisabled):
		return err.Error()
	default:
		return "Une erreur est survenue"
	}
}
