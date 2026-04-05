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
