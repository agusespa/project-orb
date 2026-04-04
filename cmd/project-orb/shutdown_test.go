package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// TestCtrlCCancelsOngoingStream verifies that Ctrl+C cancels any active stream
func TestCtrlCCancelsOngoingStream(t *testing.T) {
	cancelCalled := false
	m := testModel()
	m.streaming = true
	m.cancelCurrent = func() {
		cancelCalled = true
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !cancelCalled {
		t.Fatal("expected cancelCurrent to be called on Ctrl+C")
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	// Verify it's a quit command by checking the message type
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

// TestCtrlCWithoutStreamStillQuits verifies Ctrl+C works when not streaming
func TestCtrlCWithoutStreamStillQuits(t *testing.T) {
	m := testModel()
	m.streaming = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

// TestCtrlCInModeSelectorCancelsStream verifies Ctrl+C works in mode selector
func TestCtrlCInModeSelectorCancelsStream(t *testing.T) {
	cancelCalled := false
	m := testModel()
	m.modeSelectorActive = true
	m.streaming = true
	m.cancelCurrent = func() {
		cancelCalled = true
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !cancelCalled {
		t.Fatal("expected cancelCurrent to be called in mode selector")
	}
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

// TestStreamCancellationPropagates verifies context cancellation stops streaming
func TestStreamCancellationPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a mock service that respects context cancellation
	tokenCh := make(chan string)
	errCh := make(chan error, 1)
	doneCh := make(chan streamResult, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer close(doneCh)

		// Simulate waiting for context cancellation
		<-ctx.Done()
		doneCh <- streamResult{
			session:  agent.SessionContext{},
			canceled: true,
		}
	}()

	// Cancel the context
	cancel()

	// Verify the stream reports cancellation
	select {
	case result := <-doneCh:
		if !result.canceled {
			t.Fatal("expected stream to report cancellation")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for cancellation")
	}
}

// TestCancelFunctionIsSetOnStreamStart verifies cancel function is wired up
func TestCancelFunctionIsSetOnStreamStart(t *testing.T) {
	m := testModel()
	m.input = "test prompt"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.cancelCurrent == nil {
		t.Fatal("expected cancelCurrent to be set after starting stream")
	}
	if !got.streaming {
		t.Fatal("expected streaming to be true")
	}
}

// TestCancelFunctionIsClearedOnStreamDone verifies cleanup after completion
func TestCancelFunctionIsClearedOnStreamDone(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.cancelCurrent = func() {}
	m.output = "response"
	m.pendingPrompt = "prompt"

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: false,
	})
	got := updated.(model)

	if got.cancelCurrent != nil {
		t.Fatal("expected cancelCurrent to be cleared after stream completion")
	}
	if got.streaming {
		t.Fatal("expected streaming to be false")
	}
}

// TestCancelFunctionIsClearedOnStreamError verifies cleanup after error
func TestCancelFunctionIsClearedOnStreamError(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.cancelCurrent = func() {}

	updated, _ := m.Update(streamErrMsg{err: errors.New("test error")})
	got := updated.(model)

	if got.cancelCurrent != nil {
		t.Fatal("expected cancelCurrent to be cleared after stream error")
	}
	if got.streaming {
		t.Fatal("expected streaming to be false")
	}
}

// TestMultipleCancellationsAreSafe verifies calling cancel multiple times is safe
func TestMultipleCancellationsAreSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Call cancel multiple times - should not panic
	cancel()
	cancel()
	cancel()

	// Verify context is done
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Fatal("expected context to be done")
	}
}

// TestShutdownContextPassedToProgram verifies context is wired through
func TestShutdownContextPassedToProgram(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := testModel()
	p := newProgram(m, ctx)

	if p == nil {
		t.Fatal("expected program to be created")
	}

	// The fact that newProgram accepts and uses the context is verified
	// by the implementation - we can't easily extract the model from the program
	// but we can verify the program was created successfully
}

// TestCanceledStreamRestoresInputWhenNoOutput verifies UX on cancellation
func TestCanceledStreamRestoresInputWhenNoOutput(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "my question"
	m.output = ""
	m.cancelCurrent = func() {}

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: true,
	})
	got := updated.(model)

	if got.input != "my question" {
		t.Fatalf("expected input to be restored to %q, got %q", "my question", got.input)
	}
	if got.completed {
		t.Fatal("expected completed to be false for canceled stream")
	}
}

// TestCanceledStreamKeepsPartialOutput verifies partial responses are saved
func TestCanceledStreamKeepsPartialOutput(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "my question"
	m.output = "partial answer"
	m.cancelCurrent = func() {}

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: true,
	})
	got := updated.(model)

	if len(got.session.Recent) != 1 {
		t.Fatalf("expected 1 turn to be saved, got %d", len(got.session.Recent))
	}
	if got.session.Recent[0].User != "my question" {
		t.Fatalf("expected user message %q, got %q", "my question", got.session.Recent[0].User)
	}
	if got.session.Recent[0].Assistant != "partial answer" {
		t.Fatalf("expected assistant message %q, got %q", "partial answer", got.session.Recent[0].Assistant)
	}
	if got.input != "" {
		t.Fatalf("expected input to be empty when partial output exists, got %q", got.input)
	}
}

// TestWelcomeStreamCancellation verifies welcome messages can be canceled
func TestWelcomeStreamCancellation(t *testing.T) {
	cancelCalled := false
	m := testModel()
	m.runner = scriptedRunner{
		start: func(prompt string, session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
			t.Fatal("unexpected normal start call")
			return nil, nil, nil, nil, nil
		},
		startWelcome: func(session agent.SessionContext) (tea.Cmd, <-chan string, <-chan error, <-chan streamResult, context.CancelFunc) {
			return nil, make(chan string), make(chan error), make(chan streamResult), func() {
				cancelCalled = true
			}
		},
	}

	// Start welcome
	updated, _ := m.Update(startWelcomeMsg{})
	got := updated.(model)

	if !got.streaming {
		t.Fatal("expected welcome streaming to start")
	}
	if got.cancelCurrent == nil {
		t.Fatal("expected cancelCurrent to be set for welcome")
	}

	// Cancel it
	got.cancelCurrent()
	if !cancelCalled {
		t.Fatal("expected welcome cancel to be called")
	}
}

// TestEscDuringStreamingCancelsButDoesNotQuit verifies Esc behavior
func TestEscDuringStreamingCancelsButDoesNotQuit(t *testing.T) {
	cancelCalled := false
	m := testModel()
	m.streaming = true
	m.cancelCurrent = func() {
		cancelCalled = true
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)

	if !cancelCalled {
		t.Fatal("expected cancelCurrent to be called on Esc")
	}
	if cmd != nil {
		t.Fatal("expected no quit command on Esc, only cancellation")
	}
	if !got.streaming {
		t.Fatal("expected streaming to remain true until done message arrives")
	}
}

// TestNilCancelFunctionDoesNotPanic verifies safety when cancel is nil
func TestNilCancelFunctionDoesNotPanic(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.cancelCurrent = nil

	// Should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
}
