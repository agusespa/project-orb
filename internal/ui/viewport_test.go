package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChatViewportScrollsOnWheelMotionEvents(t *testing.T) {
	var viewport ChatViewport
	viewport.Resize(40, 12, 0)
	viewport.SetContent(strings.Repeat("wrapped viewport content\n", 30))
	viewport.GotoBottom()

	before := viewport.YOffset()
	if before == 0 {
		t.Fatal("expected viewport content to exceed the viewport before scrolling")
	}

	viewport.Update(tea.MouseMsg{
		X:      5,
		Y:      5,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonWheelUp,
	})

	if viewport.YOffset() >= before {
		t.Fatalf("expected wheel motion to scroll upward, got offset %d from %d", viewport.YOffset(), before)
	}
}
