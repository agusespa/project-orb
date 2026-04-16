package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"project-orb/internal/agent"
	"project-orb/internal/text"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type summaryRoundTripper struct{}

func (summaryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	var reqData map[string]any
	_ = json.Unmarshal(body, &reqData)

	response := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]string{
					"content": "## Overview\nFresh summary\n\n## Emotional Context\nTense\n\n## Patterns\nAvoidance\n\n## Decisions\nNone\n\n## Open Questions\nWhat next?",
				},
			},
		},
	}
	jsonData, _ := json.Marshal(response)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
		Header:     make(http.Header),
	}, nil
}

func newTestUIModel(t *testing.T, mode agent.Mode) Model {
	t.Helper()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	client, err := agent.NewClient(agent.ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: summaryRoundTripper{}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	store, err := agent.NewFileSessionStore()
	if err != nil {
		t.Fatalf("NewFileSessionStore() error = %v", err)
	}

	service, err := agent.NewService(client, mode)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	model, err := NewModel(ModelDependencies{
		Client:         client,
		CurrentMode:    mode,
		AgentName:      "Agent",
		InitialSession: agent.NewSessionContext(),
		SessionStore:   store,
	})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.stream.runner = &AgentRunner{Service: service}
	if err := model.refreshStartupMessages(context.Background()); err != nil {
		t.Fatalf("refreshStartupMessages() error = %v", err)
	}
	return model
}

func TestHandleWrapCommandPersistsAnalysisSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalysis,
		Name:         "Analysis",
		Description:  "Test analysis",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I am stuck.",
		Assistant: "Let's examine the pattern.",
		CreatedAt: time.Now().UTC(),
	})

	updated, quitCmd := model.handleWrapCommand()
	got := updated.(Model)

	if strings.TrimSpace(got.statusMessage) == "" {
		t.Fatal("expected status message after wrapping session")
	}
	if got.statusMessage != text.SessionWrapped(model.currentMode.Name) {
		t.Fatalf("expected wrap status to use centralized copy, got %q", got.statusMessage)
	}
	if !got.wrapping {
		t.Fatal("expected wrap flow to enter wrapping state")
	}
	if quitCmd == nil {
		t.Fatal("expected /wrap to return a wrap command")
	}
	if wrapMsg := quitCmd(); wrapMsg == nil {
		t.Fatal("expected wrap command to return a message")
	} else if _, ok := wrapMsg.(wrapCompleteMsg); !ok {
		t.Fatalf("expected wrapCompleteMsg, got %T", wrapMsg)
	}

	modeDir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModeAnalysis))
	entries, err := os.ReadDir(modeDir)
	if err != nil {
		t.Fatalf("expected persisted session files: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one persisted session file")
	}
}

func TestHandleCommandTreatsWrapAsUnknownInCoachMode(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeCoach,
		Name:         "Coach",
		Description:  "Test coach",
		Instructions: "Test instructions",
	})
	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I need help sorting out today.",
		Assistant: "Let's break it down.",
		CreatedAt: time.Now().UTC(),
	})

	updated, quitCmd := model.handleCommand("/wrap")
	got := updated.(Model)

	if quitCmd != nil {
		t.Fatal("expected /wrap in coach mode not to quit")
	}
	if got.err == nil || got.err.Error() != text.UnknownCommand("/wrap") {
		t.Fatalf("expected coach /wrap to be treated as unknown command, got %v", got.err)
	}

	modeDir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModeCoach))
	if _, err := os.Stat(modeDir); !os.IsNotExist(err) {
		t.Fatalf("expected coach /wrap not to persist any session data, got err=%v", err)
	}
}

func TestSwitchToModeClearsStaleErrors(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.err = errors.New("stale error")
	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 1

	updated, _ := model.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModePerformanceReview {
		t.Fatalf("expected switch to performance review, got %q", got.currentMode.ID)
	}
	if got.err != nil {
		t.Fatalf("expected successful mode switch to clear stale error, got %v", got.err)
	}
}

func TestPersistCurrentSessionUsesBackgroundFriendlyPath(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalysis,
		Name:         "Analysis",
		Description:  "Test analysis",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I keep looping.",
		Assistant: "What are you protecting?",
		CreatedAt: time.Now().UTC(),
	})

	if err := model.persistCurrentSession(context.Background()); err != nil {
		t.Fatalf("persistCurrentSession() error = %v", err)
	}
}

func TestPersistCurrentSessionSavesPerformanceReviewSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModePerformanceReview,
		Name:         "Performance Review",
		Description:  "Test performance review",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I keep starting important work late in the day.",
		Assistant: "That is a recurring execution problem, not a scheduling surprise.",
		CreatedAt: time.Now().UTC(),
	})

	if err := model.persistCurrentSession(context.Background()); err != nil {
		t.Fatalf("persistCurrentSession() error = %v", err)
	}

	currentPath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModePerformanceReview), "current.md")
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("expected current performance review summary at %s: %v", currentPath, err)
	}
}

func TestSelectHighlightedModeWarnsBeforeDiscardingUnsavedAnalysisSession(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[2])

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "Unsaved reflection.",
		Assistant: "Let's keep going.",
		CreatedAt: time.Now().UTC(),
	})
	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 0

	updated, _ := model.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModeAnalysis {
		t.Fatal("expected mode switch to be blocked")
	}
	if got.pendingSwitch != agent.ModeCoach {
		t.Fatalf("expected pending switch to coach, got %q", got.pendingSwitch)
	}
	if !strings.Contains(got.statusMessage, "switch to Coach again to discard it") {
		t.Fatalf("expected discard warning, got %q", got.statusMessage)
	}
}

func TestSelectHighlightedModeSecondAttemptDiscardsAndSwitches(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[2])

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "Unsaved reflection.",
		Assistant: "Let's keep going.",
		CreatedAt: time.Now().UTC(),
	})
	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 0

	updated, _ := model.selectHighlightedMode()
	warned := updated.(Model)
	updated, _ = warned.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModeCoach {
		t.Fatalf("expected second switch attempt to discard and switch, got %q", got.currentMode.ID)
	}
	if len(got.session.WorkingHistory) != 0 {
		t.Fatalf("expected fresh session after discard on switch, got %d turns", len(got.session.WorkingHistory))
	}
}

func TestSelectHighlightedModeWarnsBeforeDiscardingUnsavedPerformanceReviewSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModePerformanceReview,
		Name:         "Performance Review",
		Description:  "Test performance review",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I am still deferring the hard task until the afternoon.",
		Assistant: "That keeps showing up as avoidant sequencing.",
		CreatedAt: time.Now().UTC(),
	})
	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 0

	updated, _ := model.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModePerformanceReview {
		t.Fatalf("expected mode switch to be blocked, got %q", got.currentMode.ID)
	}
	if got.pendingSwitch != agent.ModeCoach {
		t.Fatalf("expected pending switch to coach, got %q", got.pendingSwitch)
	}
	if !strings.Contains(got.statusMessage, "switch to Coach again to discard it") {
		t.Fatalf("expected discard warning, got %q", got.statusMessage)
	}
}

func TestSelectHighlightedModeSecondAttemptDiscardsUnsavedPerformanceReviewSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModePerformanceReview,
		Name:         "Performance Review",
		Description:  "Test performance review",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I am still deferring the hard task until the afternoon.",
		Assistant: "That keeps showing up as avoidant sequencing.",
		CreatedAt: time.Now().UTC(),
	})
	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 0

	updated, _ := model.selectHighlightedMode()
	warned := updated.(Model)

	updated, _ = warned.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModeCoach {
		t.Fatalf("expected second switch attempt to discard and switch, got %q", got.currentMode.ID)
	}
	currentPath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModePerformanceReview), "current.md")
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("expected discarded performance review switch not to persist session data, got err=%v", err)
	}
}

func TestCtrlCWarnsBeforeDiscardingUnsavedAnalysisSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalysis,
		Name:         "Analysis",
		Description:  "Test analysis",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "Unsaved reflection.",
		Assistant: "Let's keep going.",
		CreatedAt: time.Now().UTC(),
	})

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected first Ctrl+C to warn instead of quitting")
	}
	if !got.pendingQuit {
		t.Fatal("expected pendingQuit to be set after warning")
	}
	if got.statusMessage != text.UnsavedSessionQuitMsg {
		t.Fatalf("expected discard warning, got %q", got.statusMessage)
	}
}

func TestSecondCtrlCBeginsShutdownAfterAnalysisDiscardWarning(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalysis,
		Name:         "Analysis",
		Description:  "Test analysis",
		Instructions: "Test instructions",
	})

	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "Unsaved reflection.",
		Assistant: "Let's keep going.",
		CreatedAt: time.Now().UTC(),
	})

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	warned := updated.(Model)

	updated, cmd := warned.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected second Ctrl+C to begin shutdown")
	}
	if !got.pendingQuit {
		t.Fatal("expected pendingQuit to remain set before quit")
	}
	if !got.shuttingDown {
		t.Fatal("expected shutdown to be in progress")
	}
	msg := cmd()
	shutdownMsg, ok := msg.(shutdownCompleteMsg)
	if !ok {
		t.Fatalf("expected shutdownCompleteMsg, got %T", msg)
	}
	if shutdownMsg.err != nil {
		t.Fatalf("expected shutdown to succeed, got %v", shutdownMsg.err)
	}

	next, quitCmd := got.Update(shutdownMsg)
	_ = next.(Model)
	if quitCmd == nil {
		t.Fatal("expected shutdown completion to trigger quit")
	}
	if quitMsg := quitCmd(); quitMsg == nil {
		t.Fatal("expected quit command to return a message")
	} else if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}

	modeDir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModeAnalysis))
	if _, err := os.Stat(modeDir); !os.IsNotExist(err) {
		t.Fatalf("expected analysis discard path not to persist session data, got err=%v", err)
	}
}

func TestCtrlCWarnsBeforeShutdownOutsideAnalysisMode(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	canceled := false
	model.stream.active = true
	model.stream.cancelCurrent = func() {
		canceled = true
	}

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected first Ctrl+C to warn instead of quitting")
	}
	if !got.pendingQuit {
		t.Fatal("expected pendingQuit to be set after first Ctrl+C")
	}
	if !strings.Contains(got.statusMessage, text.ShutdownWarning) {
		t.Fatalf("expected shutdown warning, got %q", got.statusMessage)
	}
	if canceled {
		t.Fatal("expected Ctrl+C not to cancel the active stream")
	}
	if got.stream.cancelCurrent == nil {
		t.Fatal("expected cancelCurrent handler to remain untouched")
	}
}

func TestSecondCtrlCStartsShutdownOutsideAnalysisMode(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	warned := updated.(Model)

	updated, cmd := warned.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected second Ctrl+C to start shutdown")
	}
	if !got.shuttingDown {
		t.Fatal("expected shuttingDown to be true")
	}
	if got.statusMessage != text.ShuttingDown {
		t.Fatalf("expected shutdown status message, got %q", got.statusMessage)
	}
}

func TestCtrlCWarnsBeforeDiscardingUnsavedPerformanceReviewSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModePerformanceReview,
		Name:         "Performance Review",
		Description:  "Test performance review",
		Instructions: "Test instructions",
	})
	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I said I would tighten the morning routine and I still have not.",
		Assistant: "That gap between stated standard and execution is the issue to watch.",
		CreatedAt: time.Now().UTC(),
	})

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected first Ctrl+C to warn instead of quitting")
	}
	if !got.pendingQuit {
		t.Fatal("expected pendingQuit to be set after warning")
	}
	if got.statusMessage != text.UnsavedSessionQuitMsg {
		t.Fatalf("expected discard warning, got %q", got.statusMessage)
	}
}

func TestSecondCtrlCDiscardsPerformanceReviewBeforeShutdown(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModePerformanceReview,
		Name:         "Performance Review",
		Description:  "Test performance review",
		Instructions: "Test instructions",
	})
	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "I said I would tighten the morning routine and I still have not.",
		Assistant: "That gap between stated standard and execution is the issue to watch.",
		CreatedAt: time.Now().UTC(),
	})

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	warned := updated.(Model)

	updated, cmd := warned.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected second Ctrl+C to start shutdown")
	}
	if !got.shuttingDown {
		t.Fatal("expected shuttingDown to be true")
	}
	if got.statusMessage != text.ShuttingDown {
		t.Fatalf("expected shutting down status immediately, got %q", got.statusMessage)
	}
	currentPath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModePerformanceReview), "current.md")
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("expected discarded performance review shutdown not to persist session data, got err=%v", err)
	}
}

func TestCtrlCWarnsBeforeShutdownInAnalysisModeWhenNoUnsavedSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalysis,
		Name:         "Analysis",
		Description:  "Test analysis",
		Instructions: "Test instructions",
	})

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected first Ctrl+C to warn instead of quitting")
	}
	if !got.pendingQuit {
		t.Fatal("expected pendingQuit to be set after first Ctrl+C")
	}
	if got.statusMessage != text.ShutdownWarning {
		t.Fatalf("expected shutdown warning, got %q", got.statusMessage)
	}
}

func TestShutdownErrorDoesNotQuit(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.shutdownFn = func() error { return errors.New("boom") }

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	warned := updated.(Model)

	updated, cmd := warned.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	inShutdown := updated.(Model)
	if cmd == nil {
		t.Fatal("expected shutdown command")
	}

	msg := cmd()
	shutdownMsg, ok := msg.(shutdownCompleteMsg)
	if !ok {
		t.Fatalf("expected shutdownCompleteMsg, got %T", msg)
	}
	if shutdownMsg.err == nil {
		t.Fatal("expected shutdown error")
	}

	next, quitCmd := inShutdown.Update(shutdownMsg)
	got := next.(Model)
	if quitCmd != nil {
		t.Fatal("expected no quit command when shutdown fails")
	}
	if got.shuttingDown {
		t.Fatal("expected shuttingDown to be false after failed shutdown")
	}
	if !strings.Contains(got.statusMessage, text.ShutdownFailed) {
		t.Fatalf("expected shutdown failure status message, got %q", got.statusMessage)
	}
}

func TestHandleStreamDoneCanceledRemovesThinkingAndRestoresInput(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	model.stream.active = true
	model.stream.waitingForFirstToken = true
	model.stream.spinnerText = ThinkingText
	model.pendingPrompt = "draft message"
	model.updateViewportContent()

	before := ansi.Strip(model.viewport.View())
	if !strings.Contains(before, ThinkingText) {
		t.Fatalf("expected viewport to show thinking state before cancel, got %q", before)
	}

	updated, _ = model.handleStreamDone(agent.StreamResult{
		Session:  model.session,
		Canceled: true,
	})
	got := updated.(Model)

	if got.input != "draft message" {
		t.Fatalf("expected canceled prompt to be restored to input, got %q", got.input)
	}
	if len(got.startupMessages) == 0 {
		t.Fatal("expected startup messages to remain after cancel with no streamed output")
	}
	after := ansi.Strip(got.viewport.View())
	if strings.Contains(after, ThinkingText) {
		t.Fatalf("expected thinking state to be removed after cancel, got %q", after)
	}
}

func TestHandleStreamDoneKeepsStartupMessagesAfterCompletedReply(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	model.pendingPrompt = "What should I focus on?"
	model.output = "Start with the highest-friction task."
	model.updateViewportContent()

	updated, _ = model.handleStreamDone(agent.StreamResult{
		Session: model.session,
	})
	got := updated.(Model)

	if len(got.startupMessages) == 0 {
		t.Fatal("expected startup messages to remain after the first completed reply")
	}

	view := ansi.Strip(got.viewport.View())
	if !strings.Contains(view, "Welcome. What situation, decision, or tension feels most") {
		t.Fatalf("expected viewport to keep startup message after completed reply, got %q", view)
	}
	if !strings.Contains(view, "What should I focus on?") {
		t.Fatalf("expected viewport to show committed user turn, got %q", view)
	}
	if !strings.Contains(view, "Start with the highest-friction task.") {
		t.Fatalf("expected viewport to show committed assistant turn, got %q", view)
	}
}

func TestWindowSizeUsesChatContentWidthForViewport(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := updated.(Model)

	if got.viewport.Width() != ChatContentWidth(80) {
		t.Fatalf("expected viewport width %d, got %d", ChatContentWidth(80), got.viewport.Width())
	}
}

func TestMouseWheelScrollsWrappedMessages(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      strings.Repeat("user message ", 20),
		Assistant: strings.Repeat("assistant reply ", 35),
		CreatedAt: time.Now().UTC(),
	})

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	model = updated.(Model)
	model.updateViewportContent()
	model.viewport.GotoBottom()

	if model.viewport.YOffset() == 0 {
		t.Fatal("expected wrapped content to exceed the viewport before scrolling")
	}

	updated, _ = model.Update(tea.MouseMsg{
		X:      5,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	got := updated.(Model)

	if got.viewport.YOffset() >= model.viewport.YOffset() {
		t.Fatalf("expected wheel up to scroll upward, got offset %d from %d", got.viewport.YOffset(), model.viewport.YOffset())
	}
}

func TestHintsCommandOpensHintsOverlay(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.input = "/hints"

	updated, _ := model.handleCommand("/hints")
	got := updated.(Model)

	if !got.hintsOverlay.active {
		t.Fatal("expected hints overlay to be active")
	}
	if got.modeSelector.active {
		t.Fatal("expected mode selector to be inactive when hints overlay opens")
	}
	if got.input != "" {
		t.Fatalf("expected input to be cleared, got %q", got.input)
	}
}

func TestEscClosesHintsOverlay(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.hintsOverlay.active = true

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if got.hintsOverlay.active {
		t.Fatal("expected esc to close hints overlay")
	}
}

func TestSyncSlashCommandUIShowsHintsOverlayForHintsInput(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.input = "/hints"
	model.modeSelector.active = true

	model.syncSlashCommandUI()

	if !model.hintsOverlay.active {
		t.Fatal("expected /hints input to open hints overlay")
	}
	if model.modeSelector.active {
		t.Fatal("expected mode selector to close when /hints is typed")
	}
}

func TestSyncSlashCommandUIShowsModeSelectorForModeInput(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.input = "/mode"
	model.hintsOverlay.active = true

	model.syncSlashCommandUI()

	if !model.modeSelector.active {
		t.Fatal("expected /mode input to open mode selector")
	}
	if model.hintsOverlay.active {
		t.Fatal("expected hints overlay to close when /mode is typed")
	}
}

func TestSyncSlashCommandUIHidesPanelsForNonMatchingCommandTokens(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.input = "/modelssssss"
	model.modeSelector.active = true
	model.hintsOverlay.active = true

	model.syncSlashCommandUI()

	if model.modeSelector.active {
		t.Fatal("expected mode selector to close for non-matching command token")
	}
	if model.hintsOverlay.active {
		t.Fatal("expected hints overlay to close for non-matching command token")
	}
}

func TestSyncSlashCommandUIShowsHintsOverlayForWrapInputInAnalysisMode(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[2])
	model.input = "/wrap"
	model.modeSelector.active = true

	model.syncSlashCommandUI()

	if !model.hintsOverlay.active {
		t.Fatal("expected /wrap input to open hints overlay in analysis mode")
	}
	if model.modeSelector.active {
		t.Fatal("expected mode selector to close when /wrap is typed")
	}
}

func TestSyncSlashCommandUIDoesNotShowHintsOverlayForWrapInputInCoachMode(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.input = "/wrap"
	model.modeSelector.active = true
	model.hintsOverlay.active = true

	model.syncSlashCommandUI()

	if model.modeSelector.active {
		t.Fatal("expected coach /wrap input not to keep mode selector open")
	}
	if model.hintsOverlay.active {
		t.Fatal("expected coach /wrap input not to show hints overlay")
	}
}

func TestNewModelShowsCoachStartupMessage(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	if len(model.startupMessages) != 1 {
		t.Fatalf("expected 1 startup message, got %d", len(model.startupMessages))
	}
	if model.startupMessages[0] != agentStartupCoachMessageForTest() {
		t.Fatalf("expected coach startup message, got %q", model.startupMessages[0])
	}
}

func TestSelectHighlightedModeRefreshesStartupMessagesForNonAnalysisMode(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[2])
	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 0

	updated, _ := model.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModeCoach {
		t.Fatalf("expected mode switch to coach, got %q", got.currentMode.ID)
	}
	if len(got.startupMessages) != 1 {
		t.Fatalf("expected 1 startup message after switch, got %d", len(got.startupMessages))
	}
	if got.startupMessages[0] != agentStartupCoachMessageForTest() {
		t.Fatalf("expected coach startup message after switch, got %q", got.startupMessages[0])
	}
}

func TestSwitchingFromCoachToAnalysisClearsVisibleConversation(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])
	model.session = agent.NewSessionContext()
	model.session.AddTurn(agent.Turn{
		User:      "Coach mode question",
		Assistant: "Coach mode answer",
		CreatedAt: time.Now().UTC(),
	})

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	model.updateViewportContent()

	before := ansi.Strip(model.viewport.View())
	if !strings.Contains(before, "Coach mode question") || !strings.Contains(before, "Coach mode answer") {
		t.Fatalf("expected viewport to show coach conversation before switch, got %q", before)
	}

	model.modeSelector.active = true
	model.input = "/mode"
	model.modeSelector.index = 2

	updated, _ = model.selectHighlightedMode()
	got := updated.(Model)

	if got.currentMode.ID != agent.ModeAnalysis {
		t.Fatalf("expected mode switch to analysis, got %q", got.currentMode.ID)
	}

	after := ansi.Strip(got.viewport.View())
	if strings.Contains(after, "Coach mode question") || strings.Contains(after, "Coach mode answer") {
		t.Fatalf("expected viewport conversation to be cleared after switch, got %q", after)
	}
}

func TestAnalysisModeLoadsFirstMessageImmediately(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	if _, err := agent.EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	client, err := agent.NewClient(agent.ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: summaryRoundTripper{}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	store, err := agent.NewFileSessionStore()
	if err != nil {
		t.Fatalf("NewFileSessionStore() error = %v", err)
	}

	analysisMode := agent.BuiltInModes()[2]
	service, err := agent.NewService(client, analysisMode)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	model, err := NewModel(ModelDependencies{
		Client:         client,
		CurrentMode:    analysisMode,
		AgentName:      "Agent",
		InitialSession: agent.NewSessionContext(),
		SessionStore:   store,
	})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.stream.runner = &AgentRunner{Service: service}

	// Simulate the Init command
	cmd := model.Init()
	msg := cmd()

	// First message should be loaded immediately
	startupMsg, ok := msg.(startupMessagesLoadedMsg)
	if !ok {
		t.Fatalf("expected startupMessagesLoadedMsg, got %T", msg)
	}
	if startupMsg.err != nil {
		t.Fatalf("expected no error, got %v", startupMsg.err)
	}
	if len(startupMsg.messages) != 1 {
		t.Fatalf("expected 1 initial message, got %d", len(startupMsg.messages))
	}
	if !strings.Contains(startupMsg.messages[0], "Welcome") {
		t.Fatalf("expected welcome message, got %q", startupMsg.messages[0])
	}

	// Update model with first message
	updated, cmd := model.Update(startupMsg)
	model = updated.(Model)

	// Should trigger loading of second message and spinner
	if cmd == nil {
		t.Fatal("expected command to load second message")
	}

	// The command is a batch, so we need to execute it to get the messages
	batchMsg := cmd()

	// Extract the batch and execute each command
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batchMsg)
	}

	// Find and execute the analysis message load command
	var analysisMsg analysisSecondMessageLoadedMsg
	for _, batchCmd := range batch {
		msg := batchCmd()
		if am, ok := msg.(analysisSecondMessageLoadedMsg); ok {
			analysisMsg = am
			break
		}
	}

	if analysisMsg.err != nil {
		t.Fatalf("expected no error loading second message, got %v", analysisMsg.err)
	}

	// Update model with second message
	updated, _ = model.Update(analysisMsg)
	model = updated.(Model)

	// Now should have 2 messages
	if len(model.startupMessages) != 2 {
		t.Fatalf("expected 2 startup messages after async load, got %d", len(model.startupMessages))
	}

	// Loading state should be cleared
	if model.loadingAnalysisMessage {
		t.Fatal("expected loading state to be cleared after message loaded")
	}
}

func agentStartupCoachMessageForTest() string {
	return "Welcome. What situation, decision, or tension feels most significant right now? We will work through it together."
}

func TestAnalysisModeShowsLoadingAnimationWhileLoadingSecondMessage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	if _, err := agent.EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	client, err := agent.NewClient(agent.ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: summaryRoundTripper{}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	store, err := agent.NewFileSessionStore()
	if err != nil {
		t.Fatalf("NewFileSessionStore() error = %v", err)
	}

	analysisMode := agent.BuiltInModes()[2]
	service, err := agent.NewService(client, analysisMode)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	model, err := NewModel(ModelDependencies{
		Client:         client,
		CurrentMode:    analysisMode,
		AgentName:      "Agent",
		InitialSession: agent.NewSessionContext(),
		SessionStore:   store,
	})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.stream.runner = &AgentRunner{Service: service}

	// Initialize viewport
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)

	// Load first message
	cmd := model.Init()
	msg := cmd()
	startupMsg := msg.(startupMessagesLoadedMsg)

	updated, _ = model.Update(startupMsg)
	model = updated.(Model)

	// Should be in loading state
	if !model.loadingAnalysisMessage {
		t.Fatal("expected loading state to be active")
	}

	// Process a spinner tick to update the animation
	updated, _ = model.Update(spinnerTickMsg{})
	model = updated.(Model)

	// Render the view and check for loading animation
	view := ansi.Strip(model.viewport.View())

	if !strings.Contains(view, "Loading memory") {
		t.Fatalf("expected loading animation to be visible, got: %q", view)
	}
}

func TestHandleKeyMovesCursorAndInsertsAtCursor(t *testing.T) {
	model := newTestUIModel(t, agent.DefaultMode())
	model.input = "helo"
	model.inputCursor = 2

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	got := updated.(Model)

	if got.input != "hello" {
		t.Fatalf("expected insertion at cursor, got %q", got.input)
	}
	if got.inputCursor != 3 {
		t.Fatalf("expected cursor to advance after insert, got %d", got.inputCursor)
	}
}

func TestHandleKeyArrowNavigationMovesCursor(t *testing.T) {
	model := newTestUIModel(t, agent.DefaultMode())
	model.input = "hello"
	model.inputCursor = len([]rune(model.input))

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	got := updated.(Model)
	if got.inputCursor != 4 {
		t.Fatalf("expected cursor to move left, got %d", got.inputCursor)
	}

	updated, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	got = updated.(Model)
	if got.inputCursor != 5 {
		t.Fatalf("expected cursor to move right, got %d", got.inputCursor)
	}
}

func TestHandleKeyBackspaceDeletesRuneBeforeCursor(t *testing.T) {
	model := newTestUIModel(t, agent.DefaultMode())
	model.input = "hello"
	model.inputCursor = 3

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(Model)

	if got.input != "helo" {
		t.Fatalf("expected backspace to delete before cursor, got %q", got.input)
	}
	if got.inputCursor != 2 {
		t.Fatalf("expected cursor to move back after delete, got %d", got.inputCursor)
	}
}
