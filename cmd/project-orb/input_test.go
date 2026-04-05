package main

import (
	"testing"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// TestInputIgnoredWhileStreaming verifies typing is blocked during streaming
func TestInputIgnoredWhileStreaming(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.input = "hello"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	got := updated.(model)

	if got.input != "hello" {
		t.Fatalf("expected input unchanged during streaming, got %q", got.input)
	}
}

// TestBackspaceHandlesEmptyInput verifies no panic on empty backspace
func TestBackspaceHandlesEmptyInput(t *testing.T) {
	m := testModel()
	m.input = ""

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(model)

	if got.input != "" {
		t.Fatalf("expected input to remain empty, got %q", got.input)
	}
}

// TestBackspaceRemovesLastCharacter verifies basic editing
func TestBackspaceRemovesLastCharacter(t *testing.T) {
	m := testModel()
	m.input = "hello"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(model)

	if got.input != "hell" {
		t.Fatalf("expected %q, got %q", "hell", got.input)
	}
}

// TestBackspaceHandlesMultibyteCharacters verifies Unicode support
func TestBackspaceHandlesMultibyteCharacters(t *testing.T) {
	m := testModel()
	m.input = "hello 世界"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(model)

	if got.input != "hello 世" {
		t.Fatalf("expected %q, got %q", "hello 世", got.input)
	}
}

// TestSpaceAddsSpace verifies space key
func TestSpaceAddsSpace(t *testing.T) {
	m := testModel()
	m.input = "hello"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	got := updated.(model)

	if got.input != "hello " {
		t.Fatalf("expected %q, got %q", "hello ", got.input)
	}
}

// TestEnterIgnoresEmptyInput verifies no action on empty enter
func TestEnterIgnoresEmptyInput(t *testing.T) {
	m := testModel()
	m.input = ""

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("expected no command for empty input")
	}
	if got.streaming {
		t.Fatal("expected streaming to remain false")
	}
}

// TestEnterIgnoresWhitespaceOnlyInput verifies trimming
func TestEnterIgnoresWhitespaceOnlyInput(t *testing.T) {
	m := testModel()
	m.input = "   "

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("expected no command for whitespace-only input")
	}
	if got.streaming {
		t.Fatal("expected streaming to remain false")
	}
}

// TestEnterStartsPromptWithValidInput verifies normal flow
func TestEnterStartsPromptWithValidInput(t *testing.T) {
	m := testModel()
	m.input = "test question"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if !got.streaming {
		t.Fatal("expected streaming to start")
	}
	if got.input != "" {
		t.Fatalf("expected input to be cleared, got %q", got.input)
	}
	if got.pendingPrompt != "test question" {
		t.Fatalf("expected pendingPrompt %q, got %q", "test question", got.pendingPrompt)
	}
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
}

// TestEnterIgnoredWhileStreaming verifies no double submission
func TestEnterIgnoredWhileStreaming(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.input = "new question"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("expected no command while already streaming")
	}
	if got.input != "new question" {
		t.Fatalf("expected input preserved, got %q", got.input)
	}
}

// TestSlashCommandDetection verifies command parsing
func TestSlashCommandDetection(t *testing.T) {
	tests := []struct {
		input     string
		isCommand bool
	}{
		{"/mode", true},
		{"/modes", true},
		{"/mode analyst", true},
		{"regular message", false},
		{" /mode", true}, // trimmed before checking
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			m := testModel()
			m.input = tt.input

			isCommand := m.isModeCommandInput()
			if isCommand != tt.isCommand {
				t.Fatalf("input %q: expected isCommand=%v, got %v", tt.input, tt.isCommand, isCommand)
			}
		})
	}
}

// TestInputClearedAfterModeSwitch verifies cleanup
func TestInputClearedAfterModeSwitch(t *testing.T) {
	m := testModel()
	m.input = "/mode analyst"
	m.modeSelectorActive = true

	newMode, _ := agent.FindMode("analyst")
	m.switchToMode(newMode, scriptedRunner{})

	if m.input != "" {
		t.Fatalf("expected input cleared after mode switch, got %q", m.input)
	}
}

// TestModeSelectorInputUpdatesMatches verifies live filtering
func TestModeSelectorInputUpdatesMatches(t *testing.T) {
	m := testModel()
	m.modeSelectorActive = true
	m.input = "/mode"

	// All modes match
	matches := m.currentModeMatches()
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches for empty query, got %d", len(matches))
	}

	// Type more to filter
	m.input = "/mode coach"
	matches = m.currentModeMatches()
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'coach', got %d", len(matches))
	}
	if matches[0].ID != agent.ModeCoach {
		t.Fatalf("expected coach mode, got %v", matches[0].ID)
	}
}
