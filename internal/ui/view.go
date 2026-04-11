package ui

import (
	"fmt"
	"math"
	"strings"

	"project-orb/internal/agent"
	"project-orb/internal/text"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	ThinkingText           = text.ThinkingText
	LoadingModelText       = text.LoadingModelText
	LoadingMemoryText      = text.LoadingMemoryText
	ChatPadding            = 2
	InputHeight            = 3
	StatusBarHeight        = 1
	WarningAreaHeight      = 3
	maxModeSelectorLines   = 10
	maxHintsOverlayLines   = 12
	messageBlockWidthRatio = 0.82

	contextGreenThreshold  = 60.0
	contextYellowThreshold = 75.0
	contextOrangeThreshold = 85.0

	ThinkingColorBright  = "255"
	ThinkingColorMedium  = "252"
	ThinkingColorDim     = "250"
	ThinkingColorSubdued = "245"
)

func ChatPaneHeight(totalHeight int, selectorHeight int) int {
	return max(1, totalHeight-InputHeight-selectorHeight-StatusBarHeight-WarningAreaHeight)
}

func TrimContentToHeight(content string, height int) string {
	if height <= 0 {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content
	}

	return strings.Join(lines[len(lines)-height:], "\n")
}

func RenderChatShell(width int, chatHeight int, chatContent string, warningMessage string, inputPane string, extraPane string, statusBar string) string {
	chatPane := RenderChatPane(width, chatHeight, chatContent)
	warningArea := RenderWarningArea(width, warningMessage)
	return RenderScreen(chatPane, warningArea, inputPane, extraPane, statusBar)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return text.LoadingPlaceholder
	}

	if !m.viewport.Ready() {
		return text.InitializingPlaceholder
	}

	statusBar := m.renderStatusBar(m.width)

	var modeSelector string
	var hintsOverlay string
	if m.modeSelector.active {
		modeSelector = m.renderModeSelector(m.width, maxModeSelectorLines)
	} else if m.hintsOverlay.active {
		hintsOverlay = m.renderHintsOverlay(m.width, maxHintsOverlayLines)
	}

	inputPane := RenderInputBox(m.width, m.styles.InputBox, ThemeForMode(m.currentMode.ID).Border, m.input, !m.stream.active && !m.loadingAnalystMessage, text.TypeYourMessagePrompt)
	extraPane := modeSelector
	if hintsOverlay != "" {
		extraPane = hintsOverlay
	}

	return RenderChatShell(m.width, m.viewport.Height(), m.viewport.View(), m.statusMessage, inputPane, extraPane, statusBar)
}

func (m Model) renderChatContent(width int) string {
	var blocks []string

	// Render startup messages with loading animation inline if needed
	for i, message := range m.startupMessages {
		if strings.TrimSpace(message) == "" {
			continue
		}

		body := message
		// If loading analyst message and this is the last startup message, append loading animation
		if m.loadingAnalystMessage && i == len(m.startupMessages)-1 {
			loadingText := RenderLoadingAnimation(m.analystLoadingFrame, text.LoadingMemoryText, ThinkingColorBright, ThinkingColorMedium, ThinkingColorDim, ThinkingColorSubdued)
			body = strings.TrimRight(body, "\n")
			if strings.TrimSpace(body) != "" {
				body += "\n\n" + loadingText
			} else {
				body = loadingText
			}
		}

		blocks = append(blocks, RenderAgentBlock(width, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, m.agentName, body))
	}

	for _, turn := range m.session.Recent {
		if strings.TrimSpace(turn.User) == "" && strings.TrimSpace(turn.Assistant) == "" {
			continue
		}
		if strings.TrimSpace(turn.User) != "" {
			blocks = append(blocks, RenderUserBlock(width, m.styles.UserNameStyle, m.styles.UserBodyStyle, text.UserLabel, turn.User))
		}
		if strings.TrimSpace(turn.Assistant) != "" {
			blocks = append(blocks, RenderAgentBlock(width, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, m.agentName, turn.Assistant))
		}
	}

	if strings.TrimSpace(m.pendingPrompt) != "" && m.stream.active {
		blocks = append(blocks, RenderUserBlock(width, m.styles.UserNameStyle, m.styles.UserBodyStyle, text.UserLabel, m.pendingPrompt))
	}

	if m.err != nil {
		blocks = append(blocks, m.styles.ErrorStyle.Render(fmt.Sprintf("%s%v", text.ErrorPrefix, m.err)))
	}

	if strings.TrimSpace(m.output) != "" || m.stream.active {
		blocks = append(blocks, m.renderCurrentAgentOutput(width))
	}

	if len(blocks) == 0 {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
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

func RenderWarningArea(width int, statusMessage string) string {
	if width <= 0 {
		return ""
	}

	warningStyle := lipgloss.NewStyle().
		Width(width).
		Height(WarningAreaHeight).
		PaddingLeft(1).
		PaddingRight(1).
		PaddingTop(1).
		PaddingBottom(0).
		Foreground(lipgloss.Color(ColorWarning)).
		Bold(true).
		AlignVertical(lipgloss.Bottom)

	status := strings.TrimSpace(statusMessage)
	if status == "" {
		return warningStyle.Render("")
	}

	availableWidth := width - ChatPadding
	if availableWidth <= 0 {
		return warningStyle.Render("")
	}

	wrapped := ansi.Wrap(status, availableWidth, "")
	lines := strings.Split(wrapped, "\n")
	maxVisibleLines := WarningAreaHeight - 1
	if len(lines) > maxVisibleLines {
		lines = lines[:maxVisibleLines]
		lines[maxVisibleLines-1] = ansi.Truncate(lines[maxVisibleLines-1], availableWidth, "")
	}

	return warningStyle.Render(strings.Join(lines, "\n"))
}

func RenderInputText(input string, interactive bool, placeholder string) string {
	switch {
	case interactive && input == "":
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorSubdued)).
			Italic(true).
			Render(InputCursor + placeholder)
	case interactive:
		return input + InputCursor
	default:
		return input
	}
}

func ChatContentWidth(width int) int {
	return width - ChatPadding
}

func RenderChatPane(width int, height int, content string) string {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Padding(0, 1, 0, 1).
		Render(content)
}

func RenderInputBox(width int, style lipgloss.Style, promptColor lipgloss.Color, input string, interactive bool, placeholder string) string {
	prompt := lipgloss.NewStyle().Foreground(promptColor).Bold(true).Render("▸ ")
	return style.Width(width).Render(prompt + RenderInputText(input, interactive, placeholder))
}

func RenderInputMessageBox(width int, style lipgloss.Style, content string) string {
	return style.Width(width).Render(content)
}

func RenderScreen(chatPane string, warningArea string, inputPane string, extraPane string, statusBar string) string {
	panes := []string{chatPane}
	panes = append(panes, warningArea)
	panes = append(panes, inputPane)
	if extraPane != "" {
		panes = append(panes, extraPane)
	}
	panes = append(panes, statusBar)
	return strings.Join(panes, "\n")
}

func RenderStatusBar(width int, style lipgloss.Style, left string, middle string, hints string) string {
	parts := []string{left}
	if middle != "" {
		parts = append(parts, middle)
	}
	if hints != "" {
		parts = append(parts, hints)
	}

	content := strings.Join(parts, " | ")
	availableWidth := max(0, width-ChatPadding)
	content = ansi.Truncate(content, availableWidth, "")
	content = " " + content + " "
	return style.Width(width).MaxHeight(1).Render(content)
}

func (m Model) renderModeSelector(width int, maxLines int) string {
	if !m.modeSelector.active || maxLines <= 2 {
		return ""
	}

	matches := m.currentModeMatches()
	var lines []string

	title := text.SelectModeTitle
	hint := text.SelectModeHint
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
		lines = append(lines, m.styles.ErrorStyle.Render(text.NoMatchingModes))
	}

	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")
	return m.styles.SelectorBoxStyle.Width(width).Render(content)
}

func hintsOverlayLines() []string {
	return []string{
		"/mode [prefix]  switch between Coach, Performance Review, Analysis",
		"/modes          alias for /mode",
		"/wrap           save current Analysis session and quit",
		"/hints          show this help panel",
		text.HintsClosePanel,
		"⇧+drag          select and copy text in supported terminals",
		text.HintsScrollConversation,
		"^C              quit",
	}
}

func (m Model) renderHintsOverlay(width int, maxLines int) string {
	if !m.hintsOverlay.active || maxLines <= 2 {
		return ""
	}

	lines := []string{
		NeutralSelectorTitleStyle.Render(text.HintsTitle) + "  " + NeutralHelpStyle.Render(text.HintsCloseHint),
	}
	lines = append(lines, hintsOverlayLines()...)

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
	hints := m.renderResponsiveStatusHints(width, modeName, contextInfo)
	middle := contextInfo
	if !statusBarFits(width, modeName, middle, hints) {
		middle = ""
		hints = m.renderResponsiveStatusHints(width, modeName, middle)
	}

	return RenderStatusBar(width, m.styles.StatusBarStyle, modeName, middle, hints)
}

func (m Model) renderResponsiveStatusHints(width int, left string, middle string) string {
	hintVariants := []string{
		"⏎ send · /hints · ^C quit",
		"/hints · ^C quit",
		"/hints",
		"^C quit",
		"",
	}

	for _, raw := range hintVariants {
		rendered := NeutralHelpStyle.Render(raw)
		if statusBarFits(width, left, middle, rendered) {
			return rendered
		}
	}

	return ""
}

func statusBarFits(width int, left string, middle string, hints string) bool {
	if width <= 0 {
		return true
	}

	parts := []string{left}
	if middle != "" {
		parts = append(parts, middle)
	}
	if hints != "" {
		parts = append(parts, hints)
	}

	content := strings.Join(parts, " | ")
	availableWidth := max(0, width-ChatPadding)
	return ansi.StringWidth(content) <= availableWidth
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

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSubdued)).Render(text.ContextLabel)
	value := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%.0f%%", percent))
	return label + value
}

func (m Model) renderThinkingSweep(frame int) string {
	spinnerText := m.stream.spinnerText
	if spinnerText == "" {
		spinnerText = text.ThinkingText
	}
	return RenderLoadingAnimation(frame, spinnerText, ThinkingColorBright, ThinkingColorMedium, ThinkingColorDim, ThinkingColorSubdued)
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
