package main

import (
	"errors"
	"testing"
	"time"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// Test that stream errors are properly handled and surfaced to the model
func TestModelHandlesStreamError(t *testing.T) {
	wantErr := errors.New("stream failed")
	m := testModel()
	m.streaming = true
	m.pendingPrompt = "test"

	updated, _ := m.Update(streamErrMsg{err: wantErr})
	got := updated.(model)

	if got.streaming {
		t.Fatal("expected streaming to stop on error")
	}
	if got.err == nil {
		t.Fatal("expected error to be set")
	}
	if !errors.Is(got.err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, got.err)
	}
	if got.waitingForFirstToken {
		t.Fatal("expected waitingForFirstToken to be false")
	}
	if got.completed {
		t.Fatal("expected completed to be false on error")
	}
}

// Test that runner errors during start are handled correctly
func TestStartPromptWithRunnerFactoryError(t *testing.T) {
	wantErr := errors.New("runner creation failed")
	m := testModel()
	m.runner = nil
	m.runnerFactory = func(mode agent.Mode) (streamRunner, error) {
		return nil, wantErr
	}
	m.input = "test prompt"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.streaming {
		t.Fatal("expected streaming to remain false")
	}
	if !errors.Is(got.err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, got.err)
	}
	if got.input != "test prompt" {
		t.Fatalf("expected input to be restored, got %q", got.input)
	}
}

// Test that nil service is handled
func TestStartStreamingNilService(t *testing.T) {
	_, _, errCh, _, cancel := startStreaming("test prompt", agent.SessionContext{}, nil)
	defer cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, errRunnerNotConfigured) {
			t.Fatalf("expected errRunnerNotConfigured, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected error from errCh")
	}
}

// Test that nil service is handled for welcome
func TestStartWelcomeStreamingNilService(t *testing.T) {
	_, _, errCh, _, cancel := startWelcomeStreaming(agent.SessionContext{}, nil)
	defer cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, errRunnerNotConfigured) {
			t.Fatalf("expected errRunnerNotConfigured, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected error from errCh")
	}
}

// Test that token messages update output correctly
func TestTokenMessageAppendsToOutput(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.waitingForFirstToken = true
	m.output = "hello"
	m.tokenCh = make(chan string)

	updated, cmd := m.Update(tokenMsg(" world"))
	got := updated.(model)

	if got.output != "hello world" {
		t.Fatalf("expected output %q, got %q", "hello world", got.output)
	}
	if got.waitingForFirstToken {
		t.Fatal("expected waitingForFirstToken to be false after first token")
	}
	if cmd == nil {
		t.Fatal("expected command to wait for next token")
	}
}

// Test that channel closed messages are handled gracefully
func TestTokenChannelClosedMessageHandled(t *testing.T) {
	m := testModel()
	m.streaming = true

	updated, cmd := m.Update(tokenChannelClosedMsg{})
	got := updated.(model)

	if !got.streaming {
		t.Fatal("expected streaming to remain true until done message")
	}
	if cmd != nil {
		t.Fatal("expected no command for channel closed message")
	}
}

func TestErrChannelClosedMessageHandled(t *testing.T) {
	m := testModel()
	m.streaming = true

	updated, cmd := m.Update(errChannelClosedMsg{})
	got := updated.(model)

	if !got.streaming {
		t.Fatal("expected streaming to remain true until done message")
	}
	if cmd != nil {
		t.Fatal("expected no command for channel closed message")
	}
}

func TestDoneChannelClosedMessageHandled(t *testing.T) {
	m := testModel()
	m.streaming = true

	updated, cmd := m.Update(doneChannelClosedMsg{})
	got := updated.(model)

	if !got.streaming {
		t.Fatal("expected streaming to remain true until done message")
	}
	if cmd != nil {
		t.Fatal("expected no command for channel closed message")
	}
}

// Test that errors clear status messages
func TestStreamErrorClearsStatusMessage(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.statusMessage = "Processing..."

	updated, _ := m.Update(streamErrMsg{err: errors.New("failed")})
	got := updated.(model)

	if got.statusMessage != "" {
		t.Fatalf("expected status message to be cleared, got %q", got.statusMessage)
	}
}

// Test that successful completion clears errors
func TestStreamDoneClearsErrors(t *testing.T) {
	m := testModel()
	m.streaming = true
	m.err = errors.New("previous error")
	m.output = "response"
	m.pendingPrompt = "prompt"

	updated, _ := m.Update(streamDoneMsg{
		session:  agent.SessionContext{},
		canceled: false,
	})
	got := updated.(model)

	if got.err != nil {
		t.Fatalf("expected error to be cleared, got %v", got.err)
	}
}
