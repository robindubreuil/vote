package models

type Message struct {
	Type         string `json:"type"`
	SessionCode  string `json:"sessionCode,omitempty"`
	TrainerToken string `json:"trainerToken,omitempty"`
	StagiaireID  string `json:"stagiaireId,omitempty"`
	// ReclaimToken (S6/S12) is the per-stagiaire secret proving
	// ownership of StagiaireID on reconnect. Forwarded to
	// VoteManager.JoinStagiaire for constant-time comparison against
	// session.ReclaimTokens[id]. Absent on first joins; the server
	// mints and returns one in session_joined.
	ReclaimToken   string            `json:"reclaimToken,omitempty"`
	Name           string            `json:"name,omitempty"`
	Colors         []string          `json:"colors,omitempty"`
	MultipleChoice bool              `json:"multipleChoice,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	GameEnabled    bool              `json:"gameEnabled,omitempty"`
	Competitive    bool              `json:"competitive,omitempty"`
	CorrectColors  []string          `json:"correctColors,omitempty"`
	AllowBlank     bool              `json:"allowBlank,omitempty"`
	GameScore      int               `json:"gameScore,omitempty"`
}

const (
	VoteStateIdle   = "idle"
	VoteStateActive = "active"
	VoteStateClosed = "closed"
)
