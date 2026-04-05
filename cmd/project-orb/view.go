package main

import (
	"fmt"
	"math"
	"strings"

	"project-orb/internal/agent"
	"project-orb/internal/ui"

	"github.com/charmbracelet/lipgloss"
)

const (
	thinkingText           = "Thinking..."
	chatPadding            = 2
	inputHeight            = 3
	statusBarHeight        = 1
	maxModeSelectorLines   = 10
	messageBlockWidthRatio = 0.82

	contextGreenThreshold  = 60.0
	contextYellowThreshold = 75.0
	contextOrangeThreshold = 85.0

	// Thinking animation colors
	thinkingColorBright  = "255"
	thinkingColorMedium  = "252"
	thinkingColorDim     = "250"
	thinkingColorSubdued = "245"
)

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	contentWidth := m.width - chatPadding
	statusBar := m.renderStatusBar(m.width)

	var modeSelector string
	if m.modeSelectorActive {
		modeSelector = m.renderModeSelector(m.width, maxModeSelectorLines)
	}

	selectorHeight := lipgloss.Height(modeSelector)
	chatHeight := max(1, m.height-inputHeight-selectorHeight-statusBarHeight)

	chatPane := lipgloss.NewStyle().
		Width(m.width).
		Height(chatHeight).
		Padding(0, 1, 2, 1).
		Render(m.renderChatContent(contentWidth, chatHeight))

	inputPane := m.inputBox.
		Width(m.width).
		Render(m.renderInputContent())

	panes := []string{chatPane, inputPane}
	if modeSelector != "" {
		panes = append(panes, modeSelector)
	}
	panes = append(panes, statusBar)

	return lipgloss.JoinVertical(lipgloss.Left, panes...)
}

func (m model) renderChatContent(width int, maxLines int) string {
	var blocks []string

	if status := strings.TrimSpace(m.statusMessage); status != "" {
		blocks = append(blocks, m.metaStyle.Render(status))
	}

	if summary := strings.TrimSpace(m.session.Summary); summary != "" {
		summaryBlock := lipgloss.JoinVertical(lipgloss.Left,
			m.summaryTitleStyle.Render("Conversation summary"),
			m.summaryBodyStyle.Render(summary),
		)
		blocks = append(blocks, summaryBlock)
	}

	for _, turn := range m.session.Recent {
		if strings.TrimSpace(turn.User) == "" && strings.TrimSpace(turn.Assistant) == "" {
			continue
		}
		if strings.TrimSpace(turn.User) != "" {
			blocks = append(blocks, renderUserBlock(width, m.userNameStyle, m.userBodyStyle, "You", turn.User))
		}
		if strings.TrimSpace(turn.Assistant) != "" {
			blocks = append(blocks, renderAgentBlock(width, m.agentNameStyle, m.agentBodyStyle, m.agentName, turn.Assistant))
		}
	}

	if strings.TrimSpace(m.pendingPrompt) != "" && m.streaming {
		blocks = append(blocks, renderUserBlock(width, m.userNameStyle, m.userBodyStyle, "You", m.pendingPrompt))
	}

	if m.err != nil {
		blocks = append(blocks, m.errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	if strings.TrimSpace(m.output) != "" || m.streaming {
		blocks = append(blocks, m.renderCurrentAgentOutput(width))
	}

	if len(blocks) == 0 {
		return ""
	}

	content := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return m.truncateToMaxLines(content, maxLines)
}

func (m model) renderCurrentAgentOutput(width int) string {
	if m.streaming && m.waitingForFirstToken {
		return renderAgentBlock(width, m.agentNameStyle, m.agentBodyStyle, m.agentName, m.renderThinkingSweep(m.spinnerFrame))
	}

	output := strings.TrimSpace(m.output)
	if output == "" {
		output = " "
	}
	return renderAgentBlock(width, m.agentNameStyle, m.agentBodyStyle, m.agentName, output)
}

func (m model) truncateToMaxLines(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func (m model) renderInputContent() string {
	text := m.input
	if text == "" && !m.streaming {
		text = ui.InputCursor + "Type your message and press Enter"
		text = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSubdued)).Italic(true).Render(text)
	} else if !m.streaming {
		text = m.input + ui.InputCursor
	}

	theme := ui.ThemeForMode(m.currentMode.ID)
	prompt := lipgloss.NewStyle().Foreground(theme.Border).Bold(true).Render("▸ ")

	return prompt + text
}

func (m model) renderModeSelector(width int, maxLines int) string {
	if !m.modeSelectorActive || maxLines <= 2 {
		return ""
	}

	matches := m.currentModeMatches()
	var lines []string

	title := "Select Mode"
	hint := "↑↓ move · ⏎ switch · esc cancel"
	titleLine := ui.NeutralSelectorTitleStyle.Render(title) + "  " + ui.NeutralHelpStyle.Render(hint)
	lines = append(lines, titleLine)

	for i, mode := range matches {
		prefix := "  "
		if i == m.modeSelectorIndex {
			prefix = "> "
		}

		nameStyle := m.getModeNameStyle(mode.ID, i)
		line := prefix + nameStyle.Render(mode.Name)
		if mode.Description != "" {
			line += " " + ui.SelectorDescriptionStyle.Render(mode.Description)
		}

		lines = append(lines, line)
	}

	if len(matches) == 0 {
		lines = append(lines, m.errorStyle.Render("No matching modes"))
	}

	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")
	return m.selectorBoxStyle.Width(width).Render(content)
}

func (m model) getModeNameStyle(modeID agent.ModeID, index int) lipgloss.Style {
	isCurrent := modeID == m.currentMode.ID
	isHighlighted := index == m.modeSelectorIndex

	if isCurrent {
		theme := ui.ThemeForMode(modeID)
		return lipgloss.NewStyle().Foreground(theme.StatusFg).Bold(true)
	}
	if isHighlighted {
		return ui.SelectorModeNameHighlightStyle
	}
	return ui.SelectorModeNameStyle
}

func (m model) renderStatusBar(width int) string {
	if width <= 0 {
		return ""
	}

	theme := ui.ThemeForMode(m.currentMode.ID)

	modeName := lipgloss.NewStyle().
		Foreground(theme.StatusFg).
		Bold(true).
		Render(m.currentMode.Name + " Mode")

	percent := m.session.ContextUsagePercent()
	contextInfo := m.renderContextUsage(percent)

	hints := ui.NeutralHelpStyle.Render("⏎ send · esc cancel · / cmd · ^C quit")

	content := strings.Join([]string{modeName, contextInfo, hints}, " | ")
	return m.statusBarStyle.Width(width).Render(content)
}

func (m model) renderContextUsage(percent float64) string {
	var color lipgloss.Color
	switch {
	case percent < contextGreenThreshold:
		color = lipgloss.Color(ui.ColorSuccess)
	case percent < contextYellowThreshold:
		color = lipgloss.Color(ui.ColorWarning)
	case percent < contextOrangeThreshold:
		color = lipgloss.Color(ui.ColorCaution)
	default:
		color = lipgloss.Color(ui.ColorDanger)
	}

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSubdued)).Render("ctx ")
	value := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%.0f%%", percent))
	return label + value
}

func (m model) renderThinkingSweep(frame int) string {
	runes := []rune(thinkingText)
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
			color = lipgloss.Color(thinkingColorBright)
		case 1:
			color = lipgloss.Color(thinkingColorMedium)
		case 2:
			color = lipgloss.Color(thinkingColorDim)
		default:
			color = lipgloss.Color(thinkingColorSubdued)
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(color).Render(string(r)))
	}

	return b.String()
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
