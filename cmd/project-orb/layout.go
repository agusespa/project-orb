package main

import "project-orb/internal/ui"

func (m model) inputPaneHeight(width int) int {
	return m.inputContentLines(width) + 2
}

func (m model) inputContentLines(width int) int {
	return len(m.renderableInputLines(width))
}

func (m model) renderableInputLines(width int) []string {
	availableWidth := max(1, width-2)
	input := m.input

	if input == "" && !m.streaming {
		placeholder := "Type your message and press Enter"
		if ui.RuneLen(placeholder)+1 <= availableWidth {
			return []string{ui.InputCursor + placeholder}
		}
		return []string{ui.InputCursor, placeholder}
	}

	lines := ui.WrapInputText(input, availableWidth)
	if len(lines) == 0 {
		lines = []string{""}
	}

	if m.streaming {
		return lines
	}

	lastLineIndex := len(lines) - 1
	if ui.RuneLen(lines[lastLineIndex]) < availableWidth {
		lines[lastLineIndex] += ui.InputCursor
		return lines
	}

	lines = append(lines, ui.InputCursor)
	return lines
}
