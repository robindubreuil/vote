package models

type Message struct {
	Type           string            `json:"type"`
	SessionCode    string            `json:"sessionCode,omitempty"`
	StagiaireID    string            `json:"stagiaireId,omitempty"`
	Name           string            `json:"name,omitempty"`
	Colors         []string          `json:"colors,omitempty"`
	MultipleChoice bool              `json:"multipleChoice,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`      // Custom labels for colors
	GameEnabled    bool              `json:"gameEnabled,omitempty"` // Mini-game enabled while trainees wait
}

const (
	VoteStateIdle   = "idle"
	VoteStateActive = "active"
	VoteStateClosed = "closed"
)
