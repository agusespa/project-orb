package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"project-orb/internal/coach"

	tea "github.com/charmbracelet/bubbletea"
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
	m.runner = scriptedRunner{start: func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
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
		session:  coach.SessionContext{},
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
		session:  coach.SessionContext{},
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
}

func TestUpdateStreamDoneCompletedStoresTurn(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "hello there"
	m.output = "full reply"

	updated, _ := m.Update(streamDoneMsg{
		session:  coach.SessionContext{},
		canceled: false,
	})
	got := updated.(model)

	if !got.completed {
		t.Fatal("expected completed to be true")
	}
	if len(got.session.Recent) != 1 {
		t.Fatalf("expected 1 stored turn, got %d", len(got.session.Recent))
	}
	if got.session.Recent[0].Assistant != "full reply" {
		t.Fatalf("unexpected stored assistant message %q", got.session.Recent[0].Assistant)
	}
	if got.output != "" {
		t.Fatalf("expected transient output to be cleared, got %q", got.output)
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
	if got.currentMode.ID != coach.ModeCoach {
		t.Fatalf("expected current mode to remain coach, got %q", got.currentMode.ID)
	}
}

func TestModeCommandSwitchesModeAndResetsSession(t *testing.T) {
	welcomeCalled := false
	m := testModel()
	m.runnerFactory = func(mode coach.Mode) (streamRunner, error) {
		return scriptedRunner{
			start: func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
			startWelcome: func(session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
				welcomeCalled = true
				return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
			},
		}, nil
	}
	m.session = coach.SessionContext{
		Summary: "old summary",
		Recent:  []coach.Turn{{User: "u", Assistant: "a"}},
	}
	m.input = "/mode analyst"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if !got.modeSelectorActive {
		t.Fatal("expected selector active after first enter")
	}

	updated, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)

	if got.currentMode.ID != coach.ModeAnalyst {
		t.Fatalf("expected analyst mode, got %q", got.currentMode.ID)
	}
	if got.coachName != "Coach" {
		t.Fatalf("expected stable coach name, got %q", got.coachName)
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
	if got.currentMode.ID != coach.ModeCoach {
		t.Fatalf("expected mode to remain coach, got %q", got.currentMode.ID)
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
	want := []string{"Type your message and press Enter█"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderableInputLines() = %#v, want %#v", got, want)
	}
}

func TestRenderStatusBarShowsCurrentMode(t *testing.T) {
	m := testModel()

	got := m.renderStatusBar(24)
	if !strings.Contains(got, "mode: coach") {
		t.Fatalf("expected status bar to show current mode, got %q", got)
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
	if !strings.Contains(got, "performance-review") {
		t.Fatalf("expected mode selector options, got %q", got)
	}
	if strings.Contains(got, "Structured feedback") {
		t.Fatalf("expected compact selector without verbose descriptions, got %q", got)
	}
}

func TestRenderModeSelectorFiltersByTypedModeName(t *testing.T) {
	m := testModel()
	m.input = "/mode ana"
	m.modeSelectorActive = true

	got := m.renderModeSelector(80)
	if !strings.Contains(got, "analyst") {
		t.Fatalf("expected analyst option, got %q", got)
	}
	if strings.Contains(got, "performance-review") {
		t.Fatalf("expected selector to filter unrelated modes, got %q", got)
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
		start: func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
			t.Fatal("unexpected normal start call")
			return nil, nil, nil, nil, nil
		},
		startWelcome: func(session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
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
	m.output = "How can I help?"

	updated, _ := m.Update(streamDoneMsg{
		session:  coach.SessionContext{},
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
}

type scriptedRunner struct {
	start        func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
	startWelcome func(session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)
}

func (r scriptedRunner) Start(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return r.start(prompt, session)
}

func (r scriptedRunner) StartWelcome(session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return r.startWelcome(session)
}

func testModel() model {
	return newModel(modelDependencies{
		runnerFactory: func(mode coach.Mode) (streamRunner, error) {
			return scriptedRunner{
				start: func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
					return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
				},
				startWelcome: func(session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
					return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
				},
			}, nil
		},
		currentMode: coach.DefaultMode(),
		coachName:   "Coach",
	})
}
