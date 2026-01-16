package models

type Message struct {
	Type           string   `json:"type"`
	SessionCode    string   `json:"sessionCode,omitempty"`
	TrainerID      string   `json:"trainerId,omitempty"`
	StagiaireID    string   `json:"stagiaireId,omitempty"`
	Name           string   `json:"name,omitempty"`
	Colors         []string `json:"colors,omitempty"`
	MultipleChoice bool     `json:"multipleChoice,omitempty"`
}

const (
	VoteStateIdle   = "idle"
	VoteStateActive = "active"
	VoteStateClosed = "closed"
)
