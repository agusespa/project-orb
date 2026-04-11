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

func TestHandleWrapCommandPersistsAnalystSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalyst,
		Name:         "Analyst",
		Description:  "Test analyst",
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
	if quitCmd == nil {
		t.Fatal("expected /wrap to return a quit command")
	}
	if quitMsg := quitCmd(); quitMsg == nil {
		t.Fatal("expected quit command to return a message")
	} else if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}

	modeDir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "project-orb", "sessions", string(agent.ModeAnalyst))
	entries, err := os.ReadDir(modeDir)
	if err != nil {
		t.Fatalf("expected persisted session files: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one persisted session file")
	}
}

func TestPersistCurrentSessionUsesBackgroundFriendlyPath(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalyst,
		Name:         "Analyst",
		Description:  "Test analyst",
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

func TestSelectHighlightedModeWarnsBeforeDiscardingUnsavedAnalystSession(t *testing.T) {
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

	if got.currentMode.ID != agent.ModeAnalyst {
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
	if len(got.session.Recent) != 0 {
		t.Fatalf("expected fresh session after discard on switch, got %d turns", len(got.session.Recent))
	}
}

func TestCtrlCWarnsBeforeDiscardingUnsavedAnalystSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalyst,
		Name:         "Analyst",
		Description:  "Test analyst",
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

func TestSecondCtrlCBeginsShutdownAfterAnalystDiscardWarning(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalyst,
		Name:         "Analyst",
		Description:  "Test analyst",
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
}

func TestCtrlCWarnsBeforeShutdownOutsideAnalystMode(t *testing.T) {
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

func TestSecondCtrlCStartsShutdownOutsideAnalystMode(t *testing.T) {
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

func TestCtrlCWarnsBeforeShutdownInAnalystModeWhenNoUnsavedSession(t *testing.T) {
	model := newTestUIModel(t, agent.Mode{
		ID:           agent.ModeAnalyst,
		Name:         "Analyst",
		Description:  "Test analyst",
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

func TestNewModelShowsCoachStartupMessage(t *testing.T) {
	model := newTestUIModel(t, agent.BuiltInModes()[0])

	if len(model.startupMessages) != 1 {
		t.Fatalf("expected 1 startup message, got %d", len(model.startupMessages))
	}
	if model.startupMessages[0] != agentStartupCoachMessageForTest() {
		t.Fatalf("expected coach startup message, got %q", model.startupMessages[0])
	}
}

func TestSelectHighlightedModeRefreshesStartupMessagesForNonAnalystMode(t *testing.T) {
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

func TestSwitchingFromCoachToAnalystClearsVisibleConversation(t *testing.T) {
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

	if got.currentMode.ID != agent.ModeAnalyst {
		t.Fatalf("expected mode switch to analyst, got %q", got.currentMode.ID)
	}

	after := ansi.Strip(got.viewport.View())
	if strings.Contains(after, "Coach mode question") || strings.Contains(after, "Coach mode answer") {
		t.Fatalf("expected viewport conversation to be cleared after switch, got %q", after)
	}
}

func TestAnalystModeLoadsFirstMessageImmediately(t *testing.T) {
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

	analystMode := agent.BuiltInModes()[2] // Analyst mode
	service, err := agent.NewService(client, analystMode)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	model, err := NewModel(ModelDependencies{
		Client:         client,
		CurrentMode:    analystMode,
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

	// Find and execute the analyst message load command
	var analystMsg analystSecondMessageLoadedMsg
	for _, batchCmd := range batch {
		msg := batchCmd()
		if am, ok := msg.(analystSecondMessageLoadedMsg); ok {
			analystMsg = am
			break
		}
	}

	if analystMsg.err != nil {
		t.Fatalf("expected no error loading second message, got %v", analystMsg.err)
	}

	// Update model with second message
	updated, _ = model.Update(analystMsg)
	model = updated.(Model)

	// Now should have 2 messages
	if len(model.startupMessages) != 2 {
		t.Fatalf("expected 2 startup messages after async load, got %d", len(model.startupMessages))
	}

	// Loading state should be cleared
	if model.loadingAnalystMessage {
		t.Fatal("expected loading state to be cleared after message loaded")
	}
}

func agentStartupCoachMessageForTest() string {
	return "Welcome. What situation, decision, or tension feels most significant right now? We will work through it together."
}

func TestAnalystModeShowsLoadingAnimationWhileLoadingSecondMessage(t *testing.T) {
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

	analystMode := agent.BuiltInModes()[2] // Analyst mode
	service, err := agent.NewService(client, analystMode)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	model, err := NewModel(ModelDependencies{
		Client:         client,
		CurrentMode:    analystMode,
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
	if !model.loadingAnalystMessage {
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
