package ui

import (
	"fmt"
	"math"
	"strings"

	"project-orb/internal/agent"

	"github.com/charmbracelet/lipgloss"
)

func renderMessageBlock(
	width int,
	nameStyle lipgloss.Style,
	textStyle lipgloss.Style,
	name string,
	text string,
	align lipgloss.Position,
) string {
	contentWidth := int(float64(width) * messageBlockWidthRatio)

	nameRendered := nameStyle.Render(name)
	messageRendered := textStyle.Render(text)

	nameBlock := lipgloss.NewStyle().Width(contentWidth).Align(align).Render(nameRendered)
	messageBlock := lipgloss.NewStyle().Width(contentWidth).Align(align).Render(messageRendered)

	joined := lipgloss.JoinVertical(lipgloss.Left, nameBlock, messageBlock)

	content := lipgloss.NewStyle().
		Width(contentWidth).
		MarginTop(1).
		Render(joined)

	return lipgloss.PlaceHorizontal(width, align, content)
}

func RenderUserBlock(width int, nameStyle, bodyStyle lipgloss.Style, label string, body string) string {
	return renderMessageBlock(width, nameStyle, bodyStyle, strings.ToUpper(label), body, lipgloss.Right)
}

func RenderAgentBlock(width int, nameStyle, bodyStyle lipgloss.Style, label string, body string) string {
	return renderMessageBlock(width, nameStyle, bodyStyle, strings.ToUpper(label), body, lipgloss.Left)
}

const (
	thinkingText           = "Thinking..."
	ChatPadding            = 2
	InputHeight            = 3
	StatusBarHeight        = 1
	maxModeSelectorLines   = 10
	messageBlockWidthRatio = 0.82

	contextGreenThreshold  = 60.0
	contextYellowThreshold = 75.0
	contextOrangeThreshold = 85.0

	ThinkingColorBright  = "255"
	ThinkingColorMedium  = "252"
	ThinkingColorDim     = "250"
	ThinkingColorSubdued = "245"
)

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	contentWidth := m.width - ChatPadding
	statusBar := m.renderStatusBar(m.width)

	var modeSelector string
	if m.modeSelector.active {
		modeSelector = m.renderModeSelector(m.width, maxModeSelectorLines)
	}

	selectorHeight := lipgloss.Height(modeSelector)
	chatHeight := max(1, m.height-InputHeight-selectorHeight-StatusBarHeight)

	chatPane := lipgloss.NewStyle().
		Width(m.width).
		Height(chatHeight).
		Padding(0, 1, 2, 1).
		Render(m.renderChatContent(contentWidth, chatHeight))

	inputPane := m.styles.InputBox.
		Width(m.width).
		Render(m.renderInputContent())

	panes := []string{chatPane, inputPane}
	if modeSelector != "" {
		panes = append(panes, modeSelector)
	}
	panes = append(panes, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, panes...)
}

func (m Model) renderChatContent(width int, maxLines int) string {
	var blocks []string

	if status := strings.TrimSpace(m.statusMessage); status != "" {
		blocks = append(blocks, m.styles.MetaStyle.Render(status))
	}

	if summary := strings.TrimSpace(m.session.Summary); summary != "" {
		summaryBlock := lipgloss.JoinVertical(lipgloss.Left,
			m.styles.SummaryTitleStyle.Render("Conversation summary"),
			m.styles.SummaryBodyStyle.Render(summary),
		)
		blocks = append(blocks, summaryBlock)
	}

	for _, turn := range m.session.Recent {
		if strings.TrimSpace(turn.User) == "" && strings.TrimSpace(turn.Assistant) == "" {
			continue
		}
		if strings.TrimSpace(turn.User) != "" {
			blocks = append(blocks, RenderUserBlock(width, m.styles.UserNameStyle, m.styles.UserBodyStyle, "You", turn.User))
		}
		if strings.TrimSpace(turn.Assistant) != "" {
			blocks = append(blocks, RenderAgentBlock(width, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, m.agentName, turn.Assistant))
		}
	}

	if strings.TrimSpace(m.pendingPrompt) != "" && m.stream.active {
		blocks = append(blocks, RenderUserBlock(width, m.styles.UserNameStyle, m.styles.UserBodyStyle, "You", m.pendingPrompt))
	}

	if m.err != nil {
		blocks = append(blocks, m.styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	if strings.TrimSpace(m.output) != "" || m.stream.active {
		blocks = append(blocks, m.renderCurrentAgentOutput(width))
	}

	if len(blocks) == 0 {
		return ""
	}

	content := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return m.truncateToMaxLines(content, maxLines)
}

func (m Model) renderCurrentAgentOutput(width int) string {
	if m.stream.active && m.stream.waitingForFirstToken {
		return RenderAgentBlock(width, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, m.agentName, m.renderThinkingSweep(m.stream.spinnerFrame))
	}

	output := strings.TrimSpace(m.output)
	if output == "" {
		output = " "
	}
	return RenderAgentBlock(width, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, m.agentName, output)
}

func (m Model) truncateToMaxLines(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func (m Model) renderInputContent() string {
	text := m.input
	if text == "" && !m.stream.active {
		text = InputCursor + "Type your message and press Enter"
		text = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubdued)).Italic(true).Render(text)
	} else if !m.stream.active {
		text = m.input + InputCursor
	}

	theme := ThemeForMode(m.currentMode.ID)
	prompt := lipgloss.NewStyle().Foreground(theme.Border).Bold(true).Render("▸ ")

	return prompt + text
}

func (m Model) renderModeSelector(width int, maxLines int) string {
	if !m.modeSelector.active || maxLines <= 2 {
		return ""
	}

	matches := m.currentModeMatches()
	var lines []string

	title := "Select Mode"
	hint := "↑↓ move · ⏎ switch · esc cancel"
	titleLine := NeutralSelectorTitleStyle.Render(title) + "  " + NeutralHelpStyle.Render(hint)
	lines = append(lines, titleLine)

	for i, mode := range matches {
		prefix := "  "
		if i == m.modeSelector.index {
			prefix = "> "
		}

		nameStyle := m.getModeNameStyle(mode.ID, i)
		line := prefix + nameStyle.Render(mode.Name)
		if mode.Description != "" {
			line += " " + SelectorDescriptionStyle.Render(mode.Description)
		}

		lines = append(lines, line)
	}

	if len(matches) == 0 {
		lines = append(lines, m.styles.ErrorStyle.Render("No matching modes"))
	}

	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")
	return m.styles.SelectorBoxStyle.Width(width).Render(content)
}

func (m Model) getModeNameStyle(modeID agent.ModeID, index int) lipgloss.Style {
	isCurrent := modeID == m.currentMode.ID
	isHighlighted := index == m.modeSelector.index

	if isCurrent {
		theme := ThemeForMode(modeID)
		return lipgloss.NewStyle().Foreground(theme.StatusFg).Bold(true)
	}
	if isHighlighted {
		return SelectorModeNameHighlightStyle
	}
	return SelectorModeNameStyle
}

func (m Model) renderStatusBar(width int) string {
	if width <= 0 {
		return ""
	}

	theme := ThemeForMode(m.currentMode.ID)

	modeName := lipgloss.NewStyle().
		Foreground(theme.StatusFg).
		Bold(true).
		Render(m.currentMode.Name + " Mode")

	percent := m.session.ContextUsagePercent()
	contextInfo := m.renderContextUsage(percent)

	hints := NeutralHelpStyle.Render("⏎ send · esc cancel · / cmd · ^C quit")

	content := strings.Join([]string{modeName, contextInfo, hints}, " | ")
	return m.styles.StatusBarStyle.Width(width).Render(content)
}

func (m Model) renderContextUsage(percent float64) string {
	var color lipgloss.Color
	switch {
	case percent < contextGreenThreshold:
		color = lipgloss.Color(ColorSuccess)
	case percent < contextYellowThreshold:
		color = lipgloss.Color(ColorWarning)
	case percent < contextOrangeThreshold:
		color = lipgloss.Color(ColorCaution)
	default:
		color = lipgloss.Color(ColorDanger)
	}

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubdued)).Render("ctx ")
	value := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%.0f%%", percent))
	return label + value
}

func (m Model) renderThinkingSweep(frame int) string {
	return RenderLoadingAnimation(frame, thinkingText, ThinkingColorBright, ThinkingColorMedium, ThinkingColorDim, ThinkingColorSubdued)
}

func RenderLoadingAnimation(frame int, text string, highlightColor, mediumColor, dimColor, subduedColor string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}

	highlight := frame % len(runes)
	var b strings.Builder

	for i, r := range runes {
		distance := int(math.Abs(float64(i - highlight)))
		var color lipgloss.Color
		switch distance {
		case 0:
			color = lipgloss.Color(highlightColor)
		case 1:
			color = lipgloss.Color(mediumColor)
		case 2:
			color = lipgloss.Color(dimColor)
		default:
			color = lipgloss.Color(subduedColor)
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(color).Render(string(r)))
	}

	// Add animated dots
	dotsCount := (frame / 3) % 4
	dots := strings.Repeat(".", dotsCount)
	spaces := strings.Repeat(" ", 3-dotsCount)
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(subduedColor)).Render(dots + spaces))

	return b.String()
}

func MatchingModes(query string) []agent.Mode {
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

func ModeQueryFromInput(input string) string {
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
