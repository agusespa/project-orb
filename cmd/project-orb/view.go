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
		Padding(0, 1).
		Render(m.renderChatContent(contentWidth, chatHeight))

	inputPane := m.inputBox.
		Width(m.width).
		Render(m.renderInputContent())

	if modeSelector == "" {
		return lipgloss.JoinVertical(lipgloss.Left, chatPane, inputPane, statusBar)
	}

	return lipgloss.JoinVertical(lipgloss.Left, chatPane, inputPane, modeSelector, statusBar)
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
		user := strings.TrimSpace(turn.User)
		assistant := strings.TrimSpace(turn.Assistant)
		if user == "" && assistant == "" {
			continue
		}
		if user != "" {
			blocks = append(blocks, m.renderUserBlock(width, "You", turn.User))
		}
		if assistant != "" {
			blocks = append(blocks, m.renderAgentBlock(width, m.agentName, turn.Assistant))
		}
	}

	if currentPrompt := strings.TrimSpace(m.pendingPrompt); currentPrompt != "" && m.streaming {
		blocks = append(blocks, m.renderUserBlock(width, "You", currentPrompt))
	}

	if m.err != nil {
		blocks = append(blocks, m.errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	currentOutput := strings.TrimSpace(m.output)
	if currentOutput != "" || m.streaming {
		if m.streaming && m.waitingForFirstToken {
			blocks = append(blocks, m.renderAgentThinking(width, m.agentName, m.spinnerFrame))
		} else if currentOutput == "" {
			blocks = append(blocks, m.renderAgentBlock(width, m.agentName, " "))
		} else {
			blocks = append(blocks, m.renderAgentBlock(width, m.agentName, currentOutput))
		}
	}

	if len(blocks) == 0 {
		return ""
	}

	content := lipgloss.JoinVertical(lipgloss.Left, blocks...)

	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		content = strings.Join(lines, "\n")
	}

	return content
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

func (m model) renderUserBlock(width int, label string, body string) string {
	contentWidth := int(float64(width) * messageBlockWidthRatio)

	name := m.userNameStyle.Render(strings.ToUpper(label))
	message := m.userBodyStyle.Width(contentWidth).Align(lipgloss.Right).Render(body)

	content := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Right).
		MarginTop(1).
		Render(lipgloss.JoinVertical(lipgloss.Right, name, message))

	return lipgloss.PlaceHorizontal(width, lipgloss.Right, content)
}

func (m model) renderAgentBlock(width int, label string, body string) string {
	contentWidth := int(float64(width) * messageBlockWidthRatio)

	name := m.agentNameStyle.Render(strings.ToUpper(label))
	message := m.agentBodyStyle.Width(contentWidth).Render(body)

	content := lipgloss.NewStyle().
		Width(contentWidth).
		MarginTop(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, name, message))

	return lipgloss.PlaceHorizontal(width, lipgloss.Left, content)
}

func (m model) renderAgentThinking(width int, label string, frame int) string {
	return m.renderAgentBlock(width, label, m.renderThinkingSweep(frame))
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
			color = lipgloss.Color("255")
		case 1:
			color = lipgloss.Color("252")
		case 2:
			color = lipgloss.Color("250")
		default:
			color = lipgloss.Color("245")
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
