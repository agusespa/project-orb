package main

import "strings"

const inputCursor = "█"

func chatContentHeight(totalHeight int) int {
	inputPaneHeight := 6
	if inputPaneHeight >= totalHeight {
		inputPaneHeight = maxInt(4, totalHeight/3)
	}
	return maxInt(3, totalHeight-inputPaneHeight)
}

func chatInnerHeight(totalHeight int) int {
	return maxInt(1, chatContentHeight(totalHeight)-4)
}

func tailLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func fitToLines(s string, maxLines int, width int) string {
	lines := wrapLines(s, width)
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxLines], "\n")
}

func wrapLines(s string, width int) []string {
	rawLines := strings.Split(s, "\n")
	var out []string

	for _, line := range rawLines {
		if line == "" {
			out = append(out, "")
			continue
		}

		runes := []rune(line)
		for len(runes) > width && width > 0 {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}

	if len(out) == 0 {
		return []string{""}
	}

	return out
}

func wrapInputText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}

	paragraphs := strings.Split(s, "\n")
	var out []string

	for _, paragraph := range paragraphs {
		if paragraph == "" {
			out = append(out, "")
			continue
		}

		tokens := splitInputTokens(paragraph)
		current := ""

		for _, token := range tokens {
			if token == "" {
				continue
			}

			candidate := current + token
			if runeLen(candidate) <= width {
				current = candidate
				continue
			}

			if current != "" {
				out = append(out, current)
				current = ""
			}

			if runeLen(token) <= width {
				current = token
				continue
			}

			runes := []rune(token)
			for len(runes) > width {
				out = append(out, string(runes[:width]))
				runes = runes[width:]
			}
			current = string(runes)
		}

		out = append(out, current)
	}

	if len(out) == 0 {
		return []string{""}
	}

	return out
}

func splitInputTokens(s string) []string {
	if s == "" {
		return nil
	}

	var tokens []string
	runes := []rune(s)
	start := 0
	inWhitespace := isWhitespace(runes[0])

	for i := 1; i < len(runes); i++ {
		if isWhitespace(runes[i]) == inWhitespace {
			continue
		}
		tokens = append(tokens, string(runes[start:i]))
		start = i
		inWhitespace = !inWhitespace
	}

	tokens = append(tokens, string(runes[start:]))
	return tokens
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t'
}

func runeLen(s string) int {
	return len([]rune(s))
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func messageBlockWidth(totalWidth int, ratio float64) int {
	width := int(float64(totalWidth) * ratio)
	return maxInt(20, width)
}

func (m model) inputPaneHeight(width int) int {
	return m.inputContentLines(width) + 2
}

func (m model) inputContentLines(width int) int {
	return len(m.renderableInputLines(width))
}

func (m model) renderableInputLines(width int) []string {
	availableWidth := maxInt(1, width-2)
	input := m.input

	if strings.TrimSpace(input) == "" && !m.streaming {
		return []string{"Type your message and press Enter"}
	}

	lines := wrapInputText(input, availableWidth)
	if len(lines) == 0 {
		lines = []string{""}
	}

	if m.streaming {
		return lines
	}

	lastLineIndex := len(lines) - 1
	if runeLen(lines[lastLineIndex]) < availableWidth {
		lines[lastLineIndex] += inputCursor
	}

	return lines
}
