package main

import (
	"strings"
	"testing"
)

func TestRenderStatusBarShowsContextUsage(t *testing.T) {
	m := testModel()

	got := m.renderStatusBar(80)

	// Check that it contains "ctx" and a percentage sign
	if !strings.Contains(got, "ctx") {
		t.Errorf("expected status bar to contain 'ctx', got %q", got)
	}
	if !strings.Contains(got, "%") {
		t.Errorf("expected status bar to contain '%%', got %q", got)
	}

	// Check that there's no format string error like %!d
	if strings.Contains(got, "%!") {
		t.Errorf("format string error detected in output: %q", got)
	}
}
