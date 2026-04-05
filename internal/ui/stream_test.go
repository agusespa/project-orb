package ui

import (
	"errors"
	"testing"

	"project-orb/internal/agent"
)

// Test tea.Cmd functions - these are UI-specific concerns

func TestWaitForTokenReturnsTokenMsg(t *testing.T) {
	ch := make(chan string, 1)
	ch <- "test-token"
	close(ch)

	cmd := waitForToken(ch)
	msg := cmd()

	tokenMsg, ok := msg.(tokenMsg)
	if !ok {
		t.Fatalf("expected tokenMsg, got %T", msg)
	}

	if string(tokenMsg) != "test-token" {
		t.Errorf("expected 'test-token', got '%s'", tokenMsg)
	}
}

func TestWaitForTokenReturnsClosedMsg(t *testing.T) {
	ch := make(chan string)
	close(ch)

	cmd := waitForToken(ch)
	msg := cmd()

	_, ok := msg.(tokenChannelClosedMsg)
	if !ok {
		t.Fatalf("expected tokenChannelClosedMsg, got %T", msg)
	}
}

func TestWaitForErrReturnsStreamErrMsg(t *testing.T) {
	ch := make(chan error, 1)
	expectedErr := errors.New("test error")
	ch <- expectedErr
	close(ch)

	cmd := waitForErr(ch)
	msg := cmd()

	errMsg, ok := msg.(streamErrMsg)
	if !ok {
		t.Fatalf("expected streamErrMsg, got %T", msg)
	}

	if !errors.Is(errMsg.err, expectedErr) {
		t.Errorf("expected test error, got %v", errMsg.err)
	}
}

func TestWaitForStreamResultReturnsDoneMsg(t *testing.T) {
	ch := make(chan agent.StreamResult, 1)
	ch <- agent.StreamResult{Canceled: true}
	close(ch)

	cmd := waitForStreamResult(ch)
	msg := cmd()

	result, ok := msg.(agent.StreamResult)
	if !ok {
		t.Fatalf("expected agent.StreamResult, got %T", msg)
	}

	if !result.Canceled {
		t.Error("expected canceled=true")
	}
}
