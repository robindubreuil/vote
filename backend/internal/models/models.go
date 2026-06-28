package models

type Message struct {
	Type           string            `json:"type"`
	SessionCode    string            `json:"sessionCode,omitempty"`
	StagiaireID    string            `json:"stagiaireId,omitempty"`
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
