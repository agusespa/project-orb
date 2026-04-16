package ui

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ChatViewport centralizes shared scroll/resize behavior for chat-like screens.
type ChatViewport struct {
	model viewport.Model
	ready bool
}

func (v *ChatViewport) Resize(totalWidth int, totalHeight int, selectorHeight int) {
	height := ChatPaneHeight(totalHeight, selectorHeight)
	width := ChatContentWidth(totalWidth)

	if !v.ready {
		v.model = viewport.New(width, height)
		v.model.YPosition = 0
		v.ready = true
		return
	}

	v.model.Width = width
	v.model.Height = height
}

func (v *ChatViewport) SetContent(content string) {
	if !v.ready {
		return
	}

	wasAtBottom := v.model.AtBottom()
	v.model.SetContent(content)
	if wasAtBottom {
		v.model.GotoBottom()
	}
}

func (v *ChatViewport) Update(msg tea.Msg) tea.Cmd {
	if !v.ready {
		return nil
	}

	if mouseMsg, ok := msg.(tea.MouseMsg); ok && tea.MouseEvent(mouseMsg).IsWheel() && mouseMsg.Action != tea.MouseActionPress {
		mouseMsg.Action = tea.MouseActionPress
		msg = mouseMsg
	}

	var cmd tea.Cmd
	v.model, cmd = v.model.Update(msg)
	return cmd
}

func (v ChatViewport) View() string {
	if !v.ready {
		return ""
	}
	return v.model.View()
}

func (v ChatViewport) Ready() bool {
	return v.ready
}

func (v ChatViewport) Width() int {
	return v.model.Width
}

func (v ChatViewport) Height() int {
	return v.model.Height
}

func (v ChatViewport) YOffset() int {
	return v.model.YOffset
}

func (v *ChatViewport) GotoBottom() {
	if !v.ready {
		return
	}
	v.model.GotoBottom()
}
