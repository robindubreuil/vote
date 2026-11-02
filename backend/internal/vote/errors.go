package vote

import "errors"

// Stable machine-readable error codes carried on the wire as
// {"type":"error","code":...,"message":...}. Clients branch on `code`
// instead of matching human-readable (and locale-prone) message text;
// `message` stays French for display. The empty string means
// "display-only, no code" and is omitted from the payload.
//
// H1 (protocol): before codes existed, the formateur UI matched the
// literal string "Session introuvable" and the stagiaire UI matched
// "Session not found" — an English string the server never sent — to
// detect session death. Any wording change silently broke identity
// recovery. Codes make that coupling explicit and testable.
const (
	CodeSessionNotFound = "session_not_found"
	CodeInvalidCode     = "invalid_session_code"
	CodeInvalidName     = "invalid_name"
	CodeNameInUse       = "name_in_use"
	CodeSessionFull     = "session_full"
	CodeNoTrainer       = "no_trainer"
	CodeRateLimited     = "rate_limited"
	CodeCreateFailed    = "session_create_failed"
	CodeShuttingDown    = "server_shutting_down"
	CodeTrainerReplaced = "trainer_replaced"
	CodeReclaimExpired  = "reclaim_expired"
)

// ErrorCode maps a manager-level error to its stable wire code, or ""
// when the error has no machine-readable contract (callers then omit
// the field and clients treat the message as display-only).
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrSessionNotFound):
		return CodeSessionNotFound
	case errors.Is(err, ErrNameInUse):
		return CodeNameInUse
	case errors.Is(err, ErrReclaimUnauthorized):
		return CodeReclaimExpired
	case errors.Is(err, ErrUnauthorized),
		errors.Is(err, ErrNotAuthorized):
		return "not_authorized"
	case errors.Is(err, ErrInvalidInput),
		errors.Is(err, ErrInvalidColor),
		errors.Is(err, ErrDuplicateColors),
		errors.Is(err, ErrBlankNotAllowed),
		errors.Is(err, ErrBlankWithColors),
		errors.Is(err, ErrAtLeastOneColor),
		errors.Is(err, ErrSingleChoiceOnly):
		return "invalid_vote"
	case errors.Is(err, ErrVoteAlreadyActive),
		errors.Is(err, ErrVoteNotActive),
		errors.Is(err, ErrVoteNotClosed):
		return "invalid_vote_state"
	default:
		return ""
	}
}

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
