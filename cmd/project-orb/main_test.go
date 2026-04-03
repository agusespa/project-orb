package main

import (
	"context"
	"errors"
	"reflect"
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
	m.runner = fakeRunner(func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
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
	})
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

func TestRenderableInputLinesDoesNotWrapFullWordJustForCursor(t *testing.T) {
	m := testModel()
	m.input = "hello world"

	got := m.renderableInputLines(13)
	want := []string{"hello world"}

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

type fakeRunner func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc)

func (f fakeRunner) Start(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
	return f(prompt, session)
}

func testModel() model {
	return newModel(modelDependencies{
		runner: fakeRunner(func(prompt string, session coach.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
			return nil, make(chan string), make(chan error), make(chan streamResult), func() {}
		}),
		coachName: "Coach",
	})
}
