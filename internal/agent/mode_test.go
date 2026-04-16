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

func TestFindModeFindsAnalysis(t *testing.T) {
	got, ok := FindMode("analysis")
	if !ok {
		t.Fatal("expected to find analysis mode")
	}
	if got.ID != ModeAnalysis {
		t.Fatalf("expected analysis mode, got %q", got.ID)
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
	analysis, ok := FindMode("analysis")
	if !ok {
		t.Fatal("expected to find analysis mode")
	}
	if !analysis.AllowsTool(toolSearchMemories) {
		t.Fatal("expected analysis mode to allow memory search")
	}
	if !analysis.AllowsTool(toolSearchMemoryTranscripts) {
		t.Fatal("expected analysis mode to allow raw transcript search")
	}
	if !analysis.AllowsTool(toolLoadMemoryTranscript) {
		t.Fatal("expected analysis mode to allow raw transcript loading")
	}

	coach := DefaultMode()
	if coach.AllowsTool(toolSearchMemories) {
		t.Fatal("expected coach mode not to allow memory search")
	}
}
