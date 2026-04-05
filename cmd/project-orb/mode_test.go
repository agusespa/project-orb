package main

import (
	"testing"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// TestModeSwitchClearsSession verifies switching modes starts fresh conversation
func TestModeSwitchClearsSession(t *testing.T) {
	m := testModel()
	m.session = agent.SessionContext{
		Summary: "old summary",
		Recent: []agent.Turn{
			{User: "old question", Assistant: "old answer"},
		},
	}

	newMode := agent.Mode{ID: agent.ModeAnalyst, Name: "Analyst"}
	m.switchToMode(newMode, scriptedRunner{})

	if m.session.Summary != "" {
		t.Fatalf("expected empty summary after mode switch, got %q", m.session.Summary)
	}
	if len(m.session.Recent) != 0 {
		t.Fatalf("expected empty recent turns after mode switch, got %d", len(m.session.Recent))
	}
	if m.currentMode.ID != agent.ModeAnalyst {
		t.Fatalf("expected mode to be Analyst, got %v", m.currentMode.ID)
	}
}

// TestModeSwitchCancelsOngoingStream verifies active streams are cancelled
func TestModeSwitchCancelsOngoingStream(t *testing.T) {
	cancelCalled := false
	m := testModel()
	m.streaming = true
	m.cancelCurrent = func() {
		cancelCalled = true
	}

	newMode := agent.Mode{ID: agent.ModeAnalyst, Name: "Analyst"}
	m.switchToMode(newMode, scriptedRunner{})

	if !cancelCalled {
		t.Fatal("expected cancelCurrent to be called when switching modes")
	}
	if m.streaming {
		t.Fatal("expected streaming to be false after mode switch")
	}
	if m.cancelCurrent != nil {
		t.Fatal("expected cancelCurrent to be nil after mode switch")
	}
}

// TestModeSwitchClearsOutput verifies output is cleared
func TestModeSwitchClearsOutput(t *testing.T) {
	m := testModel()
	m.output = "partial response"
	m.pendingPrompt = "pending question"

	newMode := agent.Mode{ID: agent.ModeAnalyst, Name: "Analyst"}
	m.switchToMode(newMode, scriptedRunner{})

	if m.output != "" {
		t.Fatalf("expected empty output after mode switch, got %q", m.output)
	}
	if m.pendingPrompt != "" {
		t.Fatalf("expected empty pendingPrompt after mode switch, got %q", m.pendingPrompt)
	}
}

// TestModeSwitchSetsStatusMessage verifies user feedback
func TestModeSwitchSetsStatusMessage(t *testing.T) {
	m := testModel()
	newMode := agent.Mode{ID: agent.ModeAnalyst, Name: "Analyst"}
	m.switchToMode(newMode, scriptedRunner{})

	if m.statusMessage == "" {
		t.Fatal("expected status message after mode switch")
	}
	if m.statusMessage != "Switched to Analyst mode. Started a fresh conversation." {
		t.Fatalf("unexpected status message: %q", m.statusMessage)
	}
}

// TestModeSelectorActivatesOnSlashMode verifies /mode command
func TestModeSelectorActivatesOnSlashMode(t *testing.T) {
	m := testModel()
	m.input = "/mode"

	m.syncSlashCommandUI()

	if !m.modeSelectorActive {
		t.Fatal("expected mode selector to activate on /mode input")
	}
}

// TestModeSelectorDeactivatesOnNonCommand verifies normal input
func TestModeSelectorDeactivatesOnNonCommand(t *testing.T) {
	m := testModel()
	m.modeSelectorActive = true
	m.input = "regular message"

	m.syncSlashCommandUI()

	if m.modeSelectorActive {
		t.Fatal("expected mode selector to deactivate on non-command input")
	}
}

// TestModeMatchingFiltersCorrectly verifies mode search
func TestModeMatchingFiltersCorrectly(t *testing.T) {
	tests := []struct {
		query string
		want  int // number of matches
	}{
		{"", 3},        // empty query matches all
		{"coach", 1},   // exact match
		{"co", 1},      // prefix match (coach)
		{"analyst", 1}, // exact match
		{"perf", 1},    // prefix match (performance-review)
		{"xyz", 0},     // no match
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			matches := matchingModes(tt.query)
			if len(matches) != tt.want {
				t.Fatalf("query %q: expected %d matches, got %d", tt.query, tt.want, len(matches))
			}
		})
	}
}

// TestModeSelectionWithEnter verifies Enter key selects mode
func TestModeSelectionWithEnter(t *testing.T) {
	m := testModel()
	m.modeSelectorActive = true
	m.input = "/mode analyst"
	m.modeSelectorIndex = 0

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.modeSelectorActive {
		t.Fatal("expected mode selector to close after selection")
	}
	if cmd == nil {
		t.Fatal("expected command to start welcome message")
	}
}

// TestModeSelectionUpDownNavigation verifies arrow key navigation
func TestModeSelectionUpDownNavigation(t *testing.T) {
	m := testModel()
	m.modeSelectorActive = true
	m.input = "/mode"
	m.modeSelectorIndex = 0

	// Press down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(model)

	if got.modeSelectorIndex != 1 {
		t.Fatalf("expected index 1 after down, got %d", got.modeSelectorIndex)
	}

	// Press up
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyUp})
	got = updated.(model)

	if got.modeSelectorIndex != 0 {
		t.Fatalf("expected index 0 after up, got %d", got.modeSelectorIndex)
	}
}

// TestModeSelectionEscCancels verifies Esc closes selector
func TestModeSelectionEscCancels(t *testing.T) {
	m := testModel()
	m.modeSelectorActive = true
	m.input = "/mode"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)

	if got.modeSelectorActive {
		t.Fatal("expected mode selector to close on Esc")
	}
}

// TestModeSelectorPreservesCurrentModeHighlight verifies UX
func TestModeSelectorPreservesCurrentModeHighlight(t *testing.T) {
	m := testModel()
	m.currentMode = agent.Mode{ID: agent.ModeAnalyst, Name: "Analyst"}
	m.input = "/mode"

	m.resetModeSelectorIndex()

	matches := m.currentModeMatches()
	if len(matches) == 0 {
		t.Fatal("expected matches for empty query")
	}

	// Should highlight current mode
	if matches[m.modeSelectorIndex].ID != agent.ModeAnalyst {
		t.Fatalf("expected current mode to be highlighted, got %v", matches[m.modeSelectorIndex].ID)
	}
}
