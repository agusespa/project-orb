package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestUpdateSpaceAppendsSpace(t *testing.T) {
	m := testModel()
	m.input = "hello"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	got := updated.(model)

	if got.input != "hello " {
		t.Fatalf("expected input %q, got %q", "hello ", got.input)
	}
}

func TestUpdateEscCancelsStreamingWithoutQuit(t *testing.T) {
	called := false
	m := testModel()
	m.streaming = true
	m.cancelCurrent = func() {
		called = true
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)

	if !called {
		t.Fatal("expected cancelCurrent to be called")
	}
	if cmd != nil {
		t.Fatal("expected no quit command when pressing escape during streaming")
	}
	if !got.streaming {
		t.Fatal("expected model to remain in streaming state until cancellation result arrives")
	}
}

func TestUpdateEnterClearsInputAndStartsStreaming(t *testing.T) {
	cancelCalled := false
	m := testModel()
	m.runner = scriptedRunner{start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
		if prompt != "hello world" {
			t.Fatalf("expected prompt %q, got %q", "hello world", prompt)
		}

		tokenCh := make(chan string)
		errCh := make(chan error)
		doneCh := make(chan streamResult)
		cancel := func() {
			cancelCalled = true
		}

		return nil, tokenCh, errCh, doneCh, cancel
	}}
	m.input = "hello world"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.input != "" {
		t.Fatalf("expected input to be cleared, got %q", got.input)
	}
	if got.pendingPrompt != "hello world" {
		t.Fatalf("expected pending prompt to be set, got %q", got.pendingPrompt)
	}
	if !got.streaming {
		t.Fatal("expected streaming to start")
	}
	if !got.waitingForFirstToken {
		t.Fatal("expected waitingForFirstToken to be true")
	}
	if got.cancelCurrent == nil {
		t.Fatal("expected cancelCurrent to be set")
	}

	got.cancelCurrent()
	if !cancelCalled {
		t.Fatal("expected returned cancel function to be wired into model")
	}
}

func TestUpdateStreamDoneCanceledRestoresPromptWhenNoOutput(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "hello there"
	m.output = ""

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: true,
	})
	got := updated.(model)

	if got.input != "hello there" {
		t.Fatalf("expected input to be restored, got %q", got.input)
	}
	if got.pendingPrompt != "" {
		t.Fatalf("expected pending prompt to be cleared, got %q", got.pendingPrompt)
	}
	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
}

func TestUpdateStreamDoneCanceledKeepsPartialOutputAsTurn(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "hello there"
	m.output = "partial reply"

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: true,
	})
	got := updated.(model)

	if len(got.session.Recent) != 1 {
		t.Fatalf("expected 1 recent turn, got %d", len(got.session.Recent))
	}
	if got.session.Recent[0].User != "hello there" {
		t.Fatalf("unexpected stored user message %q", got.session.Recent[0].User)
	}
	if got.session.Recent[0].Assistant != "partial reply" {
		t.Fatalf("unexpected stored assistant message %q", got.session.Recent[0].Assistant)
	}
	if got.input != "" {
		t.Fatalf("expected input to remain empty when partial output exists, got %q", got.input)
	}
	if got.output != "" {
		t.Fatalf("expected output to be cleared after storing turn, got %q", got.output)
	}
	if got.pendingPrompt != "" {
		t.Fatalf("expected pending prompt to be cleared, got %q", got.pendingPrompt)
	}
}

func TestUpdateStreamDoneCompletedStoresTurn(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "hello there"
	m.output = "full reply"

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: false,
	})
	got := updated.(model)

	if !got.completed {
		t.Fatal("expected completed to be true")
	}
	if len(got.session.Recent) != 1 {
		t.Fatalf("expected 1 stored turn, got %d", len(got.session.Recent))
	}
	if got.session.Recent[0].User != "hello there" {
		t.Fatalf("expected stored user message %q, got %q", "hello there", got.session.Recent[0].User)
	}
	if got.session.Recent[0].Assistant != "full reply" {
		t.Fatalf("unexpected stored assistant message %q", got.session.Recent[0].Assistant)
	}
	if got.output != "" {
		t.Fatalf("expected transient output to be cleared, got %q", got.output)
	}
	if got.pendingPrompt != "" {
		t.Fatalf("expected pending prompt to be cleared, got %q", got.pendingPrompt)
	}
	if got.streaming {
		t.Fatal("expected streaming to be false")
	}
}

func TestUpdateStreamDoneCompletedWithEmptyOutputDoesNotStoreTurn(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "hello there"
	m.output = "   "

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: false,
	})
	got := updated.(model)

	if !got.completed {
		t.Fatal("expected completed to be true")
	}
	if len(got.session.Recent) != 0 {
		t.Fatalf("expected no stored turns for empty output, got %d", len(got.session.Recent))
	}
	if got.output != "" {
		t.Fatalf("expected output to be cleared, got %q", got.output)
	}
	if got.pendingPrompt != "" {
		t.Fatalf("expected pending prompt to be cleared, got %q", got.pendingPrompt)
	}
	if got.input != "" {
		t.Fatalf("expected input to remain empty, got %q", got.input)
	}
}

func TestStartPromptWithoutRunnerRestoresInputAndSetsError(t *testing.T) {
	m := testModel()
	m.runner = nil
	m.runnerFactory = nil
	m.input = "hello world"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.input != "hello world" {
		t.Fatalf("expected input to be restored, got %q", got.input)
	}
	if !errors.Is(got.err, errRunnerNotConfigured) {
		t.Fatalf("expected runner error, got %v", got.err)
	}
	if got.streaming {
		t.Fatal("expected streaming to remain false")
	}
}

func TestModeCommandShowsAvailableModes(t *testing.T) {
	m := testModel()
	m.input = "/mode"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.input != "/mode" {
		t.Fatalf("expected input to remain /mode, got %q", got.input)
	}
	if !got.modeSelectorActive {
		t.Fatal("expected mode selector to become active")
	}
	if got.currentMode.ID != agent.ModeCoach {
		t.Fatalf("expected current mode to remain agent, got %q", got.currentMode.ID)
	}
}

func TestTypingModeCommandShowsSelectorBeforeEnter(t *testing.T) {
	m := testModel()

	for _, r := range "/mode" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(model)
	}

	if m.input != "/mode" {
		t.Fatalf("expected input /mode, got %q", m.input)
	}
	if !m.modeSelectorActive {
		t.Fatal("expected mode selector to become active while typing")
	}
}

func TestTypingModesAliasShowsSelectorBeforeEnter(t *testing.T) {
	m := testModel()

	for _, r := range "/modes" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(model)
	}

	if m.input != "/modes" {
		t.Fatalf("expected input /modes, got %q", m.input)
	}
	if !m.modeSelectorActive {
		t.Fatal("expected mode selector to become active for /modes while typing")
	}
}

func TestBackspacingOutOfModeCommandClosesSelector(t *testing.T) {
	m := testModel()
	m.input = "/mode"
	m.modeSelectorActive = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(model)

	if got.input != "/mod" {
		t.Fatalf("expected input /mod, got %q", got.input)
	}
	if got.modeSelectorActive {
		t.Fatal("expected mode selector to close when command is no longer valid")
	}
}

func TestModeCommandSwitchesModeAndResetsSession(t *testing.T) {
	welcomeCalled := false
	m := testModel()
	m.runnerFactory = func(mode agent.Mode) (streamRunner, error) {
		return scriptedRunner{
			start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
			startWelcome: func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				welcomeCalled = true
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
		}, nil
	}
	m.session = agent.SessionContext{
		Summary: "old summary",
		Recent:  []agent.Turn{{User: "u", Assistant: "a"}},
	}
	m.input = "/mode analyst"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if !got.modeSelectorActive {
		t.Fatal("expected selector active after first enter")
	}

	updated, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)

	if got.currentMode.ID != agent.ModeAnalyst {
		t.Fatalf("expected analyst mode, got %q", got.currentMode.ID)
	}
	if got.agentName != "Agent" {
		t.Fatalf("expected stable agent name, got %q", got.agentName)
	}
	if got.session.Summary != "" || len(got.session.Recent) != 0 {
		t.Fatal("expected fresh session after mode switch")
	}
	if cmd == nil {
		t.Fatal("expected follow-up welcome command")
	}

	msg := cmd()
	updated, _ = got.Update(msg)
	got = updated.(model)
	if !welcomeCalled {
		t.Fatal("expected welcome flow for new mode")
	}
	if !got.streaming {
		t.Fatal("expected streaming welcome after mode switch")
	}
}

func TestModeCommandRejectsUnknownMode(t *testing.T) {
	m := testModel()
	m.input = "/mode mystery"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)

	if got.err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if got.currentMode.ID != agent.ModeCoach {
		t.Fatalf("expected mode to remain agent, got %q", got.currentMode.ID)
	}
}

func TestModesAliasSwitchesModeAndResetsSession(t *testing.T) {
	welcomeCalled := false
	m := testModel()
	m.runnerFactory = func(mode agent.Mode) (streamRunner, error) {
		return scriptedRunner{
			start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
			startWelcome: func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				welcomeCalled = true
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
		}, nil
	}
	m.input = "/modes analyst"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if !got.modeSelectorActive {
		t.Fatal("expected selector active after entering /modes")
	}

	updated, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)

	if got.currentMode.ID != agent.ModeAnalyst {
		t.Fatalf("expected analyst mode, got %q", got.currentMode.ID)
	}
	if cmd == nil {
		t.Fatal("expected follow-up welcome command")
	}

	msg := cmd()
	updated, _ = got.Update(msg)
	got = updated.(model)
	if !welcomeCalled {
		t.Fatal("expected welcome flow for /modes alias")
	}
	if !got.streaming {
		t.Fatal("expected streaming welcome after /modes mode switch")
	}
}

func TestRenderableInputLinesDoesNotWrapFullWordJustForCursor(t *testing.T) {
	m := testModel()
	m.input = "hello world"

	got := m.renderableInputLines(13)
	want := []string{"hello world", "█"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderableInputLines() = %#v, want %#v", got, want)
	}
}

func TestRenderableInputLinesAppendsCursorWhenThereIsRoom(t *testing.T) {
	m := testModel()
	m.input = "hello"

	got := m.renderableInputLines(13)
	want := []string{"hello█"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderableInputLines() = %#v, want %#v", got, want)
	}
}

func TestRenderableInputLinesShowsCursorWithPlaceholder(t *testing.T) {
	m := testModel()

	got := m.renderableInputLines(40)
	want := []string{"█Type your message and press Enter"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderableInputLines() = %#v, want %#v", got, want)
	}
}

func TestRenderHeaderShowsCurrentMode(t *testing.T) {
	m := testModel()

	got := m.renderHeader(80)
	if !strings.Contains(got, "Agent Mode") {
		t.Fatalf("expected header to show current mode, got %q", got)
	}
}

func TestRenderModeSelectorShowsWhenTypingModeCommand(t *testing.T) {
	m := testModel()
	m.input = "/mode"
	m.modeSelectorActive = true

	got := m.renderModeSelector(80)
	if !strings.Contains(got, "Select Mode") {
		t.Fatalf("expected mode selector heading, got %q", got)
	}
	if !strings.Contains(got, "Performance Review") {
		t.Fatalf("expected mode selector options, got %q", got)
	}
	if !strings.Contains(got, "Structured feedback") {
		t.Fatalf("expected mode descriptions to be shown, got %q", got)
	}
}

func TestRenderModeSelectorFiltersByTypedModeName(t *testing.T) {
	m := testModel()
	m.input = "/mode ana"
	m.modeSelectorActive = true

	got := m.renderModeSelector(80)
	if !strings.Contains(got, "Analyst") {
		t.Fatalf("expected analyst option, got %q", got)
	}
	if strings.Contains(got, "Performance Review") {
		t.Fatalf("expected selector to filter unrelated modes, got %q", got)
	}
}

func TestViewShowsThinkingLabelWhileWaitingForFirstToken(t *testing.T) {
	m := testModel()
	m.width = 60
	m.height = 12
	m.streaming = true
	m.waitingForFirstToken = true
	m.pendingPrompt = "help me think"

	got := m.View()

	if !strings.Contains(got, "Thinking...") {
		t.Fatalf("expected thinking label in waiting state, got %q", got)
	}
}

func TestRenderUserBlockAddsSpacingBelowName(t *testing.T) {
	m := testModel()

	got := lipgloss.NewStyle().UnsetWidth().Render(m.renderUserBlock(60, "You", "hello"))

	if lipgloss.Height(got) < 3 {
		t.Fatalf("expected speaker block to include a spacer line, got %q", got)
	}
}

func TestViewFitsWithinWindowHeightWhenModeSelectorIsOpen(t *testing.T) {
	m := testModel()
	m.width = 60
	m.height = 10
	m.input = "/mode"
	m.modeSelectorActive = true
	m.session = agent.SessionContext{
		Summary: "summary line",
		Recent: []agent.Turn{
			{User: "first question", Assistant: "first answer"},
			{User: "second question", Assistant: "second answer"},
		},
	}

	got := m.View()

	if height := lipgloss.Height(got); height > m.height {
		t.Fatalf("expected rendered height <= %d, got %d\n%s", m.height, height, got)
	}
}

func TestViewFitsWithinWindowHeightWhenInputWraps(t *testing.T) {
	m := testModel()
	m.width = 32
	m.height = 9
	m.input = "this is a long input that should wrap across multiple rows in the composer"
	m.session = agent.SessionContext{
		Recent: []agent.Turn{
			{User: "hello", Assistant: "world"},
			{User: "another", Assistant: "reply"},
		},
	}

	got := m.View()

	if height := lipgloss.Height(got); height > m.height {
		t.Fatalf("expected rendered height <= %d, got %d\n%s", m.height, height, got)
	}
}

func TestViewRespectsSmallWindowWidth(t *testing.T) {
	m := testModel()
	m.width = 12
	m.height = 8
	m.session = agent.SessionContext{
		Recent: []agent.Turn{
			{User: "narrow", Assistant: "window"},
		},
	}

	got := m.View()

	for i, line := range strings.Split(got, "\n") {
		if width := lipgloss.Width(line); width > m.width {
			t.Fatalf("line %d exceeds width %d: width=%d line=%q", i+1, m.width, width, line)
		}
	}
}

func TestModeSelectorArrowKeysMoveSelection(t *testing.T) {
	m := testModel()
	m.input = "/mode"
	m.modeSelectorActive = true
	m.modeSelectorIndex = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(model)
	if got.modeSelectorIndex != 1 {
		t.Fatalf("expected selection index 1, got %d", got.modeSelectorIndex)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyUp})
	got = updated.(model)
	if got.modeSelectorIndex != 0 {
		t.Fatalf("expected selection index 0, got %d", got.modeSelectorIndex)
	}
}

func TestInitTriggersWelcomeWhenRunnerFactoryExists(t *testing.T) {
	m := testModel()
	if cmd := m.Init(); cmd == nil {
		t.Fatal("expected init command to trigger welcome")
	}
}

func TestStartWelcomeBeginsStreamingWithoutPendingPrompt(t *testing.T) {
	called := false
	m := testModel()
	m.runner = scriptedRunner{
		start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
			t.Fatal("unexpected normal start call")
			return nil, nil, nil, nil, nil
		},
		startWelcome: func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
			called = true
			return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
		},
	}

	updated, _ := m.Update(startWelcomeMsg{})
	got := updated.(model)

	if !called {
		t.Fatal("expected welcome start to be called")
	}
	if !got.streaming {
		t.Fatal("expected welcome streaming to start")
	}
	if got.pendingPrompt != "" {
		t.Fatalf("expected no pending prompt for welcome, got %q", got.pendingPrompt)
	}
}

func TestStartupWelcomeStoredWithoutUserPrompt(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = ""
	m.output = "How can I help?"

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: false,
	})
	got := updated.(model)

	if len(got.session.Recent) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(got.session.Recent))
	}
	if got.session.Recent[0].User != "" {
		t.Fatalf("expected empty user prompt, got %q", got.session.Recent[0].User)
	}
	if got.session.Recent[0].Assistant != "How can I help?" {
		t.Fatalf("expected stored welcome, got %q", got.session.Recent[0].Assistant)
	}
	if got.output != "" {
		t.Fatalf("expected output to be cleared, got %q", got.output)
	}
}

type scriptedRunner struct {
	start        func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
	startWelcome func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
}

func (r scriptedRunner) Start(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return r.start(prompt, session)
}

func (r scriptedRunner) StartWelcome(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return r.startWelcome(session)
}

func TestClockTickReturnsClockTickCommand(t *testing.T) {
	m := testModel()
	m.sessionStart = time.Now()

	updated, cmd := m.Update(clockTickMsg{})
	got := updated.(model)

	if cmd == nil {
		t.Fatal("expected clock tick to return a command")
	}
	if got.sessionStart.IsZero() {
		t.Fatal("expected session start to remain set")
	}
}

func TestClockTickReturnsNilWhenSessionNotStarted(t *testing.T) {
	m := testModel()

	updated, cmd := m.Update(clockTickMsg{})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("expected no command when session not started")
	}
	if !got.sessionStart.IsZero() {
		t.Fatal("expected session start to remain zero")
	}
}

func TestStartPromptSetsSessionStartTime(t *testing.T) {
	m := testModel()
	m.input = "hello"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.sessionStart.IsZero() {
		t.Fatal("expected session start time to be set")
	}
}

func TestStartPromptDoesNotResetSessionStartTime(t *testing.T) {
	m := testModel()
	m.input = "first"
	m.sessionStart = time.Now().Add(-5 * time.Minute)
	originalStart := m.sessionStart

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if !got.sessionStart.Equal(originalStart) {
		t.Fatal("expected session start time to remain unchanged on subsequent prompts")
	}
}

func TestStartWelcomeSetsSessionStartTime(t *testing.T) {
	m := testModel()

	updated, _ := m.Update(startWelcomeMsg{})
	got := updated.(model)

	if got.sessionStart.IsZero() {
		t.Fatal("expected session start time to be set during welcome")
	}
}

func TestModeSwitchResetsSessionStartTime(t *testing.T) {
	m := testModel()
	m.sessionStart = time.Now()
	m.runnerFactory = func(mode agent.Mode) (streamRunner, error) {
		return scriptedRunner{
			start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
			startWelcome: func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
		}, nil
	}
	m.input = "/mode analyst"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)

	if !got.sessionStart.IsZero() {
		t.Fatal("expected session start time to be reset after mode switch")
	}
}

func TestRenderHeaderShowsTimerWhenSessionActive(t *testing.T) {
	m := testModel()
	m.sessionStart = time.Now().Add(-125 * time.Second)

	got := m.renderHeader(80)

	if !strings.Contains(got, "0:02:05") {
		t.Fatalf("expected header to show '0:02:05', got %q", got)
	}
}

func TestRenderHeaderHidesTimerWhenSessionNotStarted(t *testing.T) {
	m := testModel()

	got := m.renderHeader(80)

	if strings.Contains(got, "0:00:00") {
		t.Fatalf("expected header to not show timer when session not started, got %q", got)
	}
}

func TestRenderHeaderFormatsTimerCorrectly(t *testing.T) {
	tests := []struct {
		elapsed time.Duration
		want    string
	}{
		{5 * time.Second, "0:00:05"},
		{59 * time.Second, "0:00:59"},
		{60 * time.Second, "0:01:00"},
		{90 * time.Second, "0:01:30"},
		{125 * time.Second, "0:02:05"},
		{3599 * time.Second, "0:59:59"},
		{3600 * time.Second, "1:00:00"},
		{7325 * time.Second, "2:02:05"},
	}

	for _, tt := range tests {
		m := testModel()
		m.sessionStart = time.Now().Add(-tt.elapsed)

		got := m.renderHeader(80)

		if !strings.Contains(got, tt.want) {
			t.Errorf("elapsed %v: expected header to contain %q, got %q", tt.elapsed, tt.want, got)
		}
	}
}

func testModel() model {
	return newModel(modelDependencies{
		runnerFactory: func(mode agent.Mode) (streamRunner, error) {
			return scriptedRunner{
				start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
					return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
				},
				startWelcome: func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
					return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
				},
			}, nil
		},
		currentMode: agent.DefaultMode(),
		agentName:   "Agent",
	})
}
