package agent

import (
	_ "embed"
	"errors"
	"strings"

	"project-orb/internal/text"
)

type ModeID string

const (
	ModeSetup             ModeID = "setup"
	ModeCoach             ModeID = "coach"
	ModePerformanceReview ModeID = "performance-review"
	ModeAnalyst           ModeID = "analyst"
)

type Mode struct {
	ID           ModeID
	Name         string
	Description  string
	Instructions string
	ToolNames    []string
	Selectable   bool
}

//go:embed prompts/coach_instructions.md
var coachInstructions string

//go:embed prompts/performance_review_instructions.md
var performanceReviewInstructions string

//go:embed prompts/analysis_instructions.md
var analysisInstructions string

func AllModes() []Mode {
	return []Mode{
		{
			ID:          ModeSetup,
			Name:        text.ModeSetupName,
			Description: text.ModeSetupDescription,
			Selectable:  false,
		},
		{
			ID:           ModeCoach,
			Name:         text.ModeCoachName,
			Description:  text.ModeCoachDescription,
			Instructions: coachInstructions,
			ToolNames:    nil,
			Selectable:   true,
		},
		{
			ID:           ModePerformanceReview,
			Name:         text.ModePerformanceName,
			Description:  text.ModePerformanceDescription,
			Instructions: performanceReviewInstructions,
			ToolNames:    nil,
			Selectable:   true,
		},
		{
			ID:           ModeAnalyst,
			Name:         text.ModeAnalystName,
			Description:  text.ModeAnalystDescription,
			Instructions: analysisInstructions,
			ToolNames: []string{
				toolSearchMemories,
				toolLoadMemoryExcerpt,
			},
			Selectable: true,
		},
	}
}

func BuiltInModes() []Mode {
	var modes []Mode
	for _, mode := range AllModes() {
		if mode.Selectable {
			modes = append(modes, mode)
		}
	}
	return modes
}

func DefaultMode() Mode {
	return BuiltInModes()[0]
}

func FindMode(name string) (Mode, bool) {
	normalized := strings.TrimSpace(strings.ToLower(name))
	for _, mode := range AllModes() {
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
		return "", errors.New(text.PersonaEmptyError)
	}

	modeInstructions := strings.TrimSpace(m.Instructions)
	if modeInstructions == "" {
		return "", errors.New(text.ModeInstructionsMissing(string(m.ID)))
	}

	return strings.Join([]string{
		strings.TrimSpace(embeddedInstructions),
		modeInstructions,
		persona,
	}, "\n\n---\n\n"), nil
}

func (m Mode) AllowsTool(name string) bool {
	for _, toolName := range m.ToolNames {
		if toolName == name {
			return true
		}
	}
	return false
}
