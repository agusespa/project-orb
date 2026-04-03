package main

import (
	"fmt"
	"strings"

	"project-orb/internal/coach"

	"github.com/charmbracelet/lipgloss"
)

type styles struct {
	inputBox          lipgloss.Style
	selectorBoxStyle  lipgloss.Style
	statusBarStyle    lipgloss.Style
	helpStyle         lipgloss.Style
	errorStyle        lipgloss.Style
	metaStyle         lipgloss.Style
	summaryTitleStyle lipgloss.Style
	summaryBodyStyle  lipgloss.Style
	userNameStyle     lipgloss.Style
	auraNameStyle     lipgloss.Style
	userBodyStyle     lipgloss.Style
	auraBodyStyle     lipgloss.Style
}

func newStyles() styles {
	return styles{
		inputBox: lipgloss.NewStyle().
			BorderTop(true).
			BorderBottom(true).
			BorderLeft(false).
			BorderRight(false).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1),
		selectorBoxStyle: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1),
		statusBarStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Bold(true),
		helpStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),
		errorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true),
		metaStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")),
		summaryTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("7")),
		summaryBodyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")),
		userNameStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6")),
		auraNameStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("11")),
		userBodyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),
		auraBodyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")),
	}
}

var spinnerFrames = []string{"|", "/", "-", "\\"}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	chatWidth := maxInt(20, m.width-2)
	inputWidth := maxInt(20, m.width-2)
	inputPaneHeight := m.inputPaneHeight(inputWidth - 2)
	maxInputHeight := maxInt(3, m.height/3)
	if inputPaneHeight > maxInputHeight {
		inputPaneHeight = maxInputHeight
	}
	if inputPaneHeight >= m.height {
		inputPaneHeight = maxInt(3, m.height/3)
	}
	chatPaneHeight := maxInt(3, m.height-inputPaneHeight-2)

	chatPane := lipgloss.NewStyle().
		Width(chatWidth).
		Height(maxInt(1, chatPaneHeight)).
		Padding(0, 1).
		Render(m.renderChatContent(chatWidth - 2))

	inputPane := m.inputBox.
		Width(inputWidth).
		Height(maxInt(1, inputPaneHeight-2)).
		Render(m.renderInputContent(inputWidth - 2))

	modeSelector := m.renderModeSelector(inputWidth)
	footer := m.renderFooter(inputWidth)
	if modeSelector == "" {
		return lipgloss.JoinVertical(lipgloss.Left, chatPane, inputPane, footer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, chatPane, inputPane, modeSelector, footer)
}

func (m model) renderChatContent(width int) string {
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
		b.WriteString("\n")
		if user != "" {
			b.WriteString(m.renderUserBlock(width, "You", turn.User))
			b.WriteString("\n")
		}
		if user != "" && assistant != "" {
			b.WriteString("\n")
		}
		if assistant != "" {
			b.WriteString(m.renderAuraBlock(width, m.coachName, turn.Assistant))
			b.WriteString("\n")
		}
	}

	if currentPrompt := strings.TrimSpace(m.pendingPrompt); currentPrompt != "" && m.streaming {
		b.WriteString("\n")
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
		b.WriteString("\n")
		if m.streaming && m.waitingForFirstToken {
			b.WriteString(m.renderAuraThinking(width, m.coachName, spinnerFrames[m.spinnerFrame]))
		} else if currentOutput == "" {
			b.WriteString(m.renderAuraBlock(width, m.coachName, " "))
		} else {
			b.WriteString(m.renderAuraBlock(width, m.coachName, currentOutput))
		}
	}

	if b.Len() == 0 {
		return ""
	}

	return tailLines(b.String(), maxInt(1, chatInnerHeight(m.height)))
}

func (m model) renderInputContent(width int) string {
	var b strings.Builder

	b.WriteString("› ")

	lines := m.renderableInputLines(width)
	if strings.TrimSpace(m.input) == "" && !m.streaming {
		lines[0] = m.metaStyle.Render(lines[0])
	}

	b.WriteString(strings.Join(lines, "\n  "))

	return fitToLines(b.String(), maxInt(1, m.inputContentLines(width)), width)
}

func (m model) renderFooter(width int) string {
	var lines []string

	lines = append(lines, m.renderStatusBar(width))
	lines = append(lines, m.helpStyle.Render("Enter send. `/mode` modes. Esc cancel. Ctrl+C quit."))

	if m.personaPath != "" && m.currentMode.ID == coach.ModeCoach {
		lines = append(lines, m.metaStyle.Render("Persona: "+m.personaPath))
	}

	return fitToLines(strings.Join(lines, "\n"), 3, width)
}

func (m model) renderModeSelector(width int) string {
	if !m.modeSelectorActive {
		return ""
	}

	matches := m.currentModeMatches()
	var lines []string
	lines = append(lines, m.summaryTitleStyle.Render("Select Mode"))
	for i, mode := range matches {
		prefix := "  "
		if i == m.modeSelectorIndex {
			prefix = "> "
		}
		label := string(mode.ID)
		if mode.ID == m.currentMode.ID {
			label += " *"
		}
		lines = append(lines, m.metaStyle.Render(prefix+label))
	}

	if len(matches) == 0 {
		lines = append(lines, m.errorStyle.Render("No matching modes"))
	}

	lines = append(lines, m.helpStyle.Render("Up/Down to move, Enter to switch, Esc to cancel"))

	return m.selectorBoxStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (m model) renderStatusBar(width int) string {
	content := "mode: " + string(m.currentMode.ID)
	return fitToLines(m.statusBarStyle.Render(content), 1, width)
}

func (m model) renderUserBlock(width int, label string, body string) string {
	contentWidth := messageBlockWidth(width, 0.82)
	name := m.userNameStyle.Render(label)
	message := m.userBodyStyle.Render(
		lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Right).
			Render(body),
	)
	bubble := lipgloss.NewStyle().
		MaxWidth(contentWidth).
		Align(lipgloss.Right).
		Render(name + "\n" + message)

	return lipgloss.PlaceHorizontal(width, lipgloss.Right, bubble)
}

func (m model) renderAuraBlock(width int, label string, body string) string {
	contentWidth := messageBlockWidth(width, 0.82)
	name := m.auraNameStyle.Render(label)
	message := m.auraBodyStyle.Render(
		lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Left).
			Render(body),
	)
	bubble := lipgloss.NewStyle().
		MaxWidth(contentWidth).
		Align(lipgloss.Left).
		Render(name + "\n" + message)

	return lipgloss.PlaceHorizontal(width, lipgloss.Left, bubble)
}

func (m model) renderAuraThinking(width int, label string, spinner string) string {
	contentWidth := messageBlockWidth(width, 0.82)
	name := m.auraNameStyle.Render(label)
	message := m.auraBodyStyle.Render(
		lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Left).
			Render(spinner),
	)
	bubble := lipgloss.NewStyle().
		MaxWidth(contentWidth).
		Align(lipgloss.Left).
		Render(name + "\n" + message)

	return lipgloss.PlaceHorizontal(width, lipgloss.Left, bubble)
}

func matchingModes(query string) []coach.Mode {
	if strings.TrimSpace(query) == "" {
		return coach.BuiltInModes()
	}

	var matches []coach.Mode
	for _, mode := range coach.BuiltInModes() {
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
