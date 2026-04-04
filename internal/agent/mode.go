package agent

import (
	_ "embed"
	"fmt"
	"strings"
)

type ModeID string

const (
	ModeCoach             ModeID = "coach"
	ModePerformanceReview ModeID = "performance-review"
	ModeAnalyst           ModeID = "analyst"
)

type Mode struct {
	ID           ModeID
	Name         string
	Description  string
	Instructions string
}

//go:embed prompts/agent_instructions.md
var agentInstructions string

//go:embed prompts/performance_review_instructions.md
var performanceReviewInstructions string

//go:embed prompts/analyst_instructions.md
var analystInstructions string

func BuiltInModes() []Mode {
	return []Mode{
		{
			ID:           ModeCoach,
			Name:         "Agent",
			Description:  "Guidance for everyday reflection, decisions, and next steps.",
			Instructions: agentInstructions,
		},
		{
			ID:           ModePerformanceReview,
			Name:         "Performance Review",
			Description:  "Structured feedback on effectiveness, habits and growth areas.",
			Instructions: performanceReviewInstructions,
		},
		{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Deeper psychoanalytic questioning to examine motives and patterns.",
			Instructions: analystInstructions,
		},
	}
}

func DefaultMode() Mode {
	return BuiltInModes()[0]
}

func FindMode(name string) (Mode, bool) {
	normalized := strings.TrimSpace(strings.ToLower(name))
	for _, mode := range BuiltInModes() {
		if string(mode.ID) == normalized {
			return mode, true
		}
	}

	return Mode{}, false
}

func (m Mode) SystemMessage() (string, error) {
	persona, err := LoadPersona()
	if err != nil {
		return "", err
	}
	persona = strings.TrimSpace(persona)

	if persona == "" {
		return "", fmt.Errorf("persona is empty")
	}

	modeInstructions := strings.TrimSpace(m.Instructions)
	if modeInstructions == "" {
		return "", fmt.Errorf("mode %q has no instructions configured", m.ID)
	}

	return strings.Join([]string{
		strings.TrimSpace(embeddedInstructions),
		modeInstructions,
		persona,
	}, "\n\n---\n\n"), nil
}
