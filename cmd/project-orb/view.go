package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"project-orb/internal/agent"
	"project-orb/internal/ui"

	"github.com/charmbracelet/lipgloss"
)

const thinkingText = "Thinking..."

var thinkingFrames = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	contentWidth := max(1, m.width)

	header := m.renderHeader(contentWidth)
	headerHeight := renderedLineCount(header)

	footerMaxLines := max(0, m.height-2-headerHeight)
	footer := m.renderFooterWithLimit(contentWidth, footerMaxLines)
	footerHeight := renderedLineCount(footer)

	remainingHeight := max(2, m.height-footerHeight-headerHeight)
	desiredInputPaneHeight := m.inputPaneHeight(contentWidth - 2)
	maxInputHeight := max(3, remainingHeight/3)
	inputPaneHeight := min(max(desiredInputPaneHeight, 1), max(1, min(maxInputHeight, remainingHeight-1)))

	selectorMaxLines := 0
	if m.modeSelectorActive {
		selectorMaxLines = max(0, remainingHeight-inputPaneHeight-1)
	}
	modeSelector := m.renderModeSelectorWithLimit(contentWidth, selectorMaxLines)
	selectorHeight := renderedLineCount(modeSelector)
	chatPaneHeight := max(1, remainingHeight-inputPaneHeight-selectorHeight)

	chatPane := lipgloss.NewStyle().
		Width(contentWidth).
		Height(chatPaneHeight).
		Padding(0, 1).
		Render(m.renderChatContent(contentWidth-2, chatPaneHeight))

	inputPane := m.inputBox.
		Width(contentWidth).
		Height(max(1, inputPaneHeight-2)).
		Render(m.renderInputContent(contentWidth-2, max(1, inputPaneHeight-2)))

	if modeSelector == "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, chatPane, inputPane, footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, chatPane, inputPane, modeSelector, footer)
}

func (m model) renderChatContent(width int, maxLines int) string {
	var b strings.Builder

	if status := strings.TrimSpace(m.statusMessage); status != "" {
		b.WriteString(m.metaStyle.Render(status))
		b.WriteString("\n")
	}

	if summary := strings.TrimSpace(m.session.Summary); summary != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.summaryTitleStyle.Render("Conversation summary"))
		b.WriteString("\n")
		b.WriteString(m.summaryBodyStyle.Render(summary))
		b.WriteString("\n")
	}

	for _, turn := range m.session.Recent {
		user := strings.TrimSpace(turn.User)
		assistant := strings.TrimSpace(turn.Assistant)
		if user == "" && assistant == "" {
			continue
		}
		b.WriteString("\n\n")
		if user != "" {
			b.WriteString(m.renderUserBlock(width, "You", turn.User))
			b.WriteString("\n")
		}
		if user != "" && assistant != "" {
			b.WriteString("\n\n")
		}
		if assistant != "" {
			b.WriteString(m.renderCoachBlock(width, m.coachName, turn.Assistant))
			b.WriteString("\n")
		}
	}

	if currentPrompt := strings.TrimSpace(m.pendingPrompt); currentPrompt != "" && m.streaming {
		b.WriteString("\n\n")
		b.WriteString(m.renderUserBlock(width, "You", currentPrompt))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(m.errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
	}

	currentOutput := strings.TrimSpace(m.output)
	if currentOutput != "" || m.streaming {
		b.WriteString("\n\n")
		if m.streaming && m.waitingForFirstToken {
			b.WriteString(m.renderCoachThinking(width, m.coachName, thinkingFrames[m.spinnerFrame]))
		} else if currentOutput == "" {
			b.WriteString(m.renderCoachBlock(width, m.coachName, " "))
		} else {
			b.WriteString(m.renderCoachBlock(width, m.coachName, currentOutput))
		}
	}

	if b.Len() == 0 {
		return ""
	}

	return ui.TailLines(b.String(), max(1, maxLines))
}

func (m model) renderInputContent(width int, maxLines int) string {
	var b strings.Builder

	b.WriteString("› ")

	lines := m.renderableInputLines(width)
	if m.input == "" && !m.streaming {
		lines[0] = ui.NeutralMetaStyle.Render(lines[0])
	}

	b.WriteString(strings.Join(lines, "\n  "))

	return ui.FitToLines(b.String(), max(1, maxLines), width)
}

func (m model) renderFooter(width int) string {
	return m.renderFooterWithLimit(width, 3)
}

func (m model) renderFooterWithLimit(width int, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}

	var lines []string

	lines = append(lines, ui.NeutralHelpStyle.Render("Press Enter to send, Esc to cancel. '/' to enter commands. Ctrl+C to quit."))

	return ui.FitToLines(strings.Join(lines, "\n"), maxLines, width)
}

func (m model) renderModeSelector(width int) string {
	// Each mode takes 1 line, plus title line
	return m.renderModeSelectorWithLimit(width, max(1, len(agent.BuiltInModes())+2))
}

func (m model) renderModeSelectorWithLimit(width int, maxLines int) string {
	if !m.modeSelectorActive {
		return ""
	}
	if maxLines <= 2 {
		return ""
	}

	matches := m.currentModeMatches()
	var lines []string

	// Title with hint on the same line
	title := "Select Mode"
	hint := "↑↓ move · ⏎ switch · esc cancel"
	titleLine := ui.NeutralSelectorTitleStyle.Render(title) + "  " + ui.NeutralHelpStyle.Render(hint)
	lines = append(lines, titleLine)

	for i, mode := range matches {
		prefix := "  "
		if i == m.modeSelectorIndex {
			prefix = "> "
		}

		// Choose style based on whether it's current mode and/or highlighted
		var nameStyle lipgloss.Style
		isCurrent := mode.ID == m.currentMode.ID
		isHighlighted := i == m.modeSelectorIndex

		if isCurrent {
			// Current mode uses its theme color, bold
			theme := ui.ThemeForMode(mode.ID)
			nameStyle = lipgloss.NewStyle().
				Foreground(theme.StatusFg).
				Bold(true)
		} else if isHighlighted {
			// Highlighted mode uses white, bold
			nameStyle = ui.SelectorModeNameHighlightStyle
		} else {
			// Other modes use regular gray
			nameStyle = ui.SelectorModeNameStyle
		}

		// Build the line: prefix + name + description
		line := prefix + nameStyle.Render(mode.Name)
		if mode.Description != "" {
			line += " " + ui.SelectorDescriptionStyle.Render(mode.Description)
		}

		lines = append(lines, line)
	}

	if len(matches) == 0 {
		lines = append(lines, m.errorStyle.Render("No matching modes"))
	}

	content := ui.FitToLines(strings.Join(lines, "\n"), maxLines-2, width)
	if content == "" {
		return ""
	}

	return m.selectorBoxStyle.Width(width).Render(content)
}

func (m model) renderHeader(width int) string {
	if width <= 0 {
		return ""
	}

	centerText := m.currentMode.Name + " Mode"

	var rightText string
	if !m.sessionStart.IsZero() {
		elapsed := time.Since(m.sessionStart).Round(time.Second)
		hours := int(elapsed.Hours())
		minutes := int(elapsed.Minutes()) % 60
		seconds := int(elapsed.Seconds()) % 60
		rightText = ui.NeutralMetaStyle.Render(fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds))
	}

	// Build a three-section layout: empty | center | right
	// Each section shares equal width so center is visually centered.
	sectionWidth := width / 3
	remainder := width - sectionWidth*3

	left := lipgloss.NewStyle().Width(sectionWidth).Render("")
	center := lipgloss.NewStyle().
		Width(sectionWidth + remainder).
		Align(lipgloss.Center).
		Bold(true).
		Render(ui.FitToLines(centerText, 1, sectionWidth+remainder))
	right := lipgloss.NewStyle().
		Width(sectionWidth).
		Align(lipgloss.Right).
		Render(rightText)

	row := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	return m.statusBarStyle.Width(width).Render(row)
}

func renderedLineCount(s string) int {
	if s == "" {
		return 0
	}

	return lipgloss.Height(s)
}

func (m model) renderUserBlock(width int, label string, body string) string {
	contentWidth := ui.MessageBlockWidth(width, 0.82)
	name := m.renderSpeakerName(m.userNameStyle, label)
	message := m.userBodyStyle.Render(
		lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Right).
			Render(body),
	)
	bubble := lipgloss.NewStyle().
		MaxWidth(contentWidth).
		Align(lipgloss.Right).
		Render(name + "\n\n" + message)

	return lipgloss.PlaceHorizontal(width, lipgloss.Right, bubble)
}

func (m model) renderCoachBlock(width int, label string, body string) string {
	contentWidth := ui.MessageBlockWidth(width, 0.82)
	name := m.renderSpeakerName(m.coachNameStyle, label)
	message := m.coachBodyStyle.Render(
		lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Left).
			Render(body),
	)
	bubble := lipgloss.NewStyle().
		MaxWidth(contentWidth).
		Align(lipgloss.Left).
		Render(name + "\n\n" + message)

	return lipgloss.PlaceHorizontal(width, lipgloss.Left, bubble)
}

func (m model) renderCoachThinking(width int, label string, frame int) string {
	contentWidth := ui.MessageBlockWidth(width, 0.82)
	name := m.renderSpeakerName(m.coachNameStyle, label)
	message := m.coachBodyStyle.Render(
		lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Left).
			Render(m.renderThinkingSweep(frame)),
	)
	bubble := lipgloss.NewStyle().
		MaxWidth(contentWidth).
		Align(lipgloss.Left).
		Render(name + "\n\n" + message)

	return lipgloss.PlaceHorizontal(width, lipgloss.Left, bubble)
}

func (m model) renderSpeakerName(style lipgloss.Style, label string) string {
	return style.Render(strings.ToUpper(label))
}

func (m model) renderThinkingSweep(frame int) string {
	runes := []rune(thinkingText)
	if len(runes) == 0 {
		return ""
	}

	highlight := frame % len(runes)
	var b strings.Builder

	for i, r := range runes {
		color := thinkingSweepColor(int(math.Abs(float64(i - highlight))))
		b.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(color).
			Render(string(r)))
	}

	return b.String()
}

func thinkingSweepColor(distance int) lipgloss.Color {
	switch distance {
	case 0:
		return lipgloss.Color("255")
	case 1:
		return lipgloss.Color("252")
	case 2:
		return lipgloss.Color("250")
	default:
		return lipgloss.Color("245")
	}
}

func matchingModes(query string) []agent.Mode {
	if strings.TrimSpace(query) == "" {
		return agent.BuiltInModes()
	}

	var matches []agent.Mode
	for _, mode := range agent.BuiltInModes() {
		id := string(mode.ID)
		if strings.HasPrefix(id, strings.ToLower(strings.TrimSpace(query))) {
			matches = append(matches, mode)
		}
	}

	return matches
}

func modeQueryFromInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/mode") {
		return ""
	}

	fields := strings.Fields(trimmed)
	if len(fields) <= 1 {
		return ""
	}

	return fields[1]
}
