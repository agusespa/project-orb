package ui

import (
	"strings"
	"testing"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderInputTextShowsPlaceholderOnlyWhenInteractive(t *testing.T) {
	rendered := RenderInputText("", true, "Type your message and press Enter")
	if !strings.Contains(rendered, InputCursor+"Type your message and press Enter") {
		t.Fatalf("expected placeholder with attached cursor, got %q", rendered)
	}

	rendered = RenderInputText("", false, "Type your message and press Enter")
	if rendered != "" {
		t.Fatalf("expected no placeholder while disabled, got %q", rendered)
	}
}

func TestRenderInputTextAppendsCursorWithoutExtraSpace(t *testing.T) {
	rendered := RenderInputText("hello", true, "ignored")
	if !strings.Contains(rendered, "hello"+InputCursor) {
		t.Fatalf("expected cursor attached to input, got %q", rendered)
	}
	if strings.Contains(rendered, "hello "+InputCursor) {
		t.Fatalf("expected no extra space before cursor, got %q", rendered)
	}
}

func TestRenderInputBoxIncludesPromptAndSharedContent(t *testing.T) {
	rendered := RenderInputBox(40, lipgloss.NewStyle(), lipgloss.Color("7"), "", true, "Type your message and press Enter")
	if !strings.Contains(rendered, "▸ ") {
		t.Fatalf("expected prompt in input box, got %q", rendered)
	}
	if !strings.Contains(rendered, InputCursor+"Type your message and press Enter") {
		t.Fatalf("expected shared placeholder rendering, got %q", rendered)
	}
}

func TestRenderInputMessageBoxUsesSharedWrapper(t *testing.T) {
	rendered := RenderInputMessageBox(40, lipgloss.NewStyle(), "Press Ctrl+C to exit")
	if !strings.Contains(rendered, "Press Ctrl+C to exit") {
		t.Fatalf("expected static message content, got %q", rendered)
	}
}

func TestChatPaneHeightReservesSharedLayoutAreas(t *testing.T) {
	got := ChatPaneHeight(24, 0)
	want := 24 - InputHeight - StatusBarHeight - WarningAreaHeight
	if got != want {
		t.Fatalf("expected chat height %d, got %d", want, got)
	}
}

func TestTrimContentToHeightKeepsLatestLines(t *testing.T) {
	got := TrimContentToHeight("1\n2\n3\n4", 2)
	if got != "3\n4" {
		t.Fatalf("expected latest lines, got %q", got)
	}
}

func TestRenderStatusBarStaysSingleLineWhenNarrow(t *testing.T) {
	model := Model{
		currentMode: agent.DefaultMode(),
		session:     agent.NewSessionContext(),
		styles:      NewStyles(agent.ModeCoach),
	}

	rendered := model.renderStatusBar(20)
	if lipgloss.Height(rendered) != 1 {
		t.Fatalf("expected single-line status bar, got height %d", lipgloss.Height(rendered))
	}
}

func TestRenderStatusBarCollapsesHintsWhenNarrow(t *testing.T) {
	model := Model{
		currentMode: agent.DefaultMode(),
		session:     agent.NewSessionContext(),
		styles:      NewStyles(agent.ModeCoach),
	}

	rendered := ansi.Strip(model.renderStatusBar(40))
	if strings.Contains(rendered, "⇧+drag select") || strings.Contains(rendered, "/ cmd") {
		t.Fatalf("expected inline hints to stay concise, got %q", rendered)
	}
	if !strings.Contains(rendered, "/hints") {
		t.Fatalf("expected /hints discoverability to remain visible, got %q", rendered)
	}
}

func TestRenderStatusBarShowsHintsCommandWhenWide(t *testing.T) {
	model := Model{
		currentMode: agent.DefaultMode(),
		session:     agent.NewSessionContext(),
		styles:      NewStyles(agent.ModeCoach),
	}

	rendered := ansi.Strip(model.renderStatusBar(120))
	if !strings.Contains(rendered, "/hints") {
		t.Fatalf("expected /hints discoverability in status bar, got %q", rendered)
	}
}

func TestRenderHintsOverlayShowsCommands(t *testing.T) {
	model := Model{
		currentMode:  agent.DefaultMode(),
		session:      agent.NewSessionContext(),
		styles:       NewStyles(agent.ModeCoach),
		hintsOverlay: hintsOverlay{active: true},
	}

	rendered := ansi.Strip(model.renderHintsOverlay(100, maxHintsOverlayLines))
	if !strings.Contains(rendered, "/hints") {
		t.Fatalf("expected hints overlay to include /hints, got %q", rendered)
	}
	if !strings.Contains(rendered, "/wrap") {
		t.Fatalf("expected hints overlay to include /wrap, got %q", rendered)
	}
}

func TestViewMatchesWindowHeight(t *testing.T) {
	model := Model{
		width:       80,
		height:      24,
		currentMode: agent.DefaultMode(),
		session:     agent.NewSessionContext(),
		styles:      NewStyles(agent.ModeCoach),
	}

	// Initialize viewport by simulating a window size message
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)

	rendered := model.View()
	if lipgloss.Height(rendered) != model.height {
		t.Fatalf("expected view height %d, got %d", model.height, lipgloss.Height(rendered))
	}
	if !strings.Contains(rendered, "Type your message and press Enter") {
		t.Fatalf("expected rendered view to include input pane, got %q", rendered)
	}
}

func TestWindowSizeKeepsViewportHeightStableWithWarningArea(t *testing.T) {
	base := Model{
		currentMode: agent.DefaultMode(),
		session:     agent.NewSessionContext(),
		styles:      NewStyles(agent.ModeCoach),
	}

	updated, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	withoutWarning := updated.(Model)

	base.statusMessage = "Unsaved session warning"
	updated, _ = base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	withWarning := updated.(Model)

	if withoutWarning.viewport.Height() != withWarning.viewport.Height() {
		t.Fatalf("expected viewport height to stay stable, got %d without warning and %d with warning", withoutWarning.viewport.Height(), withWarning.viewport.Height())
	}
}

func TestRenderWarningAreaAlignsWithChatPanePadding(t *testing.T) {
	model := Model{statusMessage: "Unsaved session warning"}

	rendered := ansi.Strip(RenderWarningArea(40, model.statusMessage))
	lines := strings.Split(rendered, "\n")
	if len(lines) != WarningAreaHeight {
		t.Fatalf("expected warning area height %d, got %d", WarningAreaHeight, len(lines))
	}

	lastLine := lines[len(lines)-1]
	if !strings.HasPrefix(lastLine, " "+model.statusMessage) {
		t.Fatalf("expected warning text to start with single-column padding, got %q", lastLine)
	}
	if strings.HasPrefix(lastLine, "  "+model.statusMessage) {
		t.Fatalf("expected warning text not to have extra left padding, got %q", lastLine)
	}
}

func TestRenderWarningAreaWrapsLongMessageAcrossTwoLines(t *testing.T) {
	message := "Unsaved Analysis session will be discarded. Use /wrap to save it, or press Ctrl+C again to quit without saving."

	rendered := ansi.Strip(RenderWarningArea(40, message))
	lines := strings.Split(rendered, "\n")
	if len(lines) != WarningAreaHeight {
		t.Fatalf("expected warning area height %d, got %d", WarningAreaHeight, len(lines))
	}

	if strings.TrimSpace(lines[1]) == "" {
		t.Fatalf("expected wrapped warning text on the first visible line, got %q", lines[1])
	}
	if strings.TrimSpace(lines[2]) == "" {
		t.Fatalf("expected wrapped warning text on the second visible line, got %q", lines[2])
	}

	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, " ") {
			t.Fatalf("expected warning text to keep single-column left padding, got %q", line)
		}
	}
}

func TestRenderChatContentDoesNotDisplaySavedSummaryBlock(t *testing.T) {
	model := Model{
		currentMode: agent.Mode{
			ID:           agent.ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
		agentName: "Claudio",
		styles:    NewStyles(agent.ModeAnalyst),
		session: agent.SessionContext{
			Summary: "## Overview\nThis should stay internal.",
		},
		startupMessages: []string{"Welcome back."},
	}

	content := ansi.Strip(model.renderChatContent(80))
	if strings.Contains(content, "Conversation summary") {
		t.Fatalf("expected saved summary heading not to be rendered, got %q", content)
	}
	if strings.Contains(content, "This should stay internal.") {
		t.Fatalf("expected saved summary body not to be rendered, got %q", content)
	}
}
