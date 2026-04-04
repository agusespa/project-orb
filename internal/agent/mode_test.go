package agent

import (
	"strings"
	"testing"
)

func TestDefaultModeIsCoach(t *testing.T) {
	got := DefaultMode()
	if got.ID != ModeCoach {
		t.Fatalf("expected agent default mode, got %q", got.ID)
	}
}

func TestFindModeFindsAnalyst(t *testing.T) {
	got, ok := FindMode("analyst")
	if !ok {
		t.Fatal("expected to find analyst mode")
	}
	if got.ID != ModeAnalyst {
		t.Fatalf("expected analyst mode, got %q", got.ID)
	}
}

func TestNonCoachModeBuildsSystemMessage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	mode, ok := FindMode("performance-review")
	if !ok {
		t.Fatal("expected to find performance-review mode")
	}

	systemMessage, err := mode.SystemMessage()
	if err != nil {
		t.Fatalf("SystemMessage() error = %v", err)
	}

	if !strings.Contains(systemMessage, "These instructions define how you think") {
		t.Fatalf("expected global instructions in system message, got %q", systemMessage)
	}
	if !strings.Contains(systemMessage, "honest performance review") {
		t.Fatalf("expected mode instructions in system message, got %q", systemMessage)
	}
	if !strings.Contains(systemMessage, "calm, practical AI life coach") {
		t.Fatalf("expected shared persona in system message, got %q", systemMessage)
	}
}
