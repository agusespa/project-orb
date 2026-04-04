package ui

import "strings"

const InputCursor = "█"

func ChatContentHeight(totalHeight int) int {
	inputPaneHeight := 6
	if inputPaneHeight >= totalHeight {
		inputPaneHeight = max(4, totalHeight/3)
	}
	return max(3, totalHeight-inputPaneHeight)
}

func ChatInnerHeight(totalHeight int) int {
	return max(1, ChatContentHeight(totalHeight)-4)
}

func TailLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func FitToLines(s string, maxLines int, width int) string {
	lines := WrapLines(s, width)
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxLines], "\n")
}

func WrapLines(s string, width int) []string {
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

func WrapInputText(s string, width int) []string {
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
			if RuneLen(candidate) <= width {
				current = candidate
				continue
			}

			if current != "" {
				out = append(out, current)
				current = ""
			}

			if RuneLen(token) <= width {
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

func RuneLen(s string) int {
	return len([]rune(s))
}

func MessageBlockWidth(totalWidth int, ratio float64) int {
	if totalWidth <= 0 {
		return 1
	}

	width := int(float64(totalWidth) * ratio)
	width = max(20, width)
	return min(totalWidth, width)
}
