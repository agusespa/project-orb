package agent

import (
	"testing"
)

func TestDefaultModeIsCoach(t *testing.T) {
	got := DefaultMode()
	if got.ID != ModeCoach {
		t.Fatalf("expected coach default mode, got %q", got.ID)
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

func TestFindModeFindsSetup(t *testing.T) {
	got, ok := FindMode("setup")
	if !ok {
		t.Fatal("expected to find setup mode")
	}
	if got.ID != ModeSetup {
		t.Fatalf("expected setup mode, got %q", got.ID)
	}
	if got.Selectable {
		t.Fatal("expected setup mode not to be selectable from /mode")
	}
}

func TestModeAllowsToolRespectsModeCapabilities(t *testing.T) {
	analyst, ok := FindMode("analyst")
	if !ok {
		t.Fatal("expected to find analyst mode")
	}
	if !analyst.AllowsTool(toolSearchMemories) {
		t.Fatal("expected analyst mode to allow memory search")
	}

	coach := DefaultMode()
	if coach.AllowsTool(toolSearchMemories) {
		t.Fatal("expected coach mode not to allow memory search")
	}
}
