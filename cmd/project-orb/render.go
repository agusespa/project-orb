package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderMessageBlock renders a message block with consistent styling
// align should be lipgloss.Left for agent messages, lipgloss.Right for user messages
func renderMessageBlock(
	width int,
	nameStyle lipgloss.Style,
	textStyle lipgloss.Style,
	name string,
	text string,
	align lipgloss.Position,
) string {
	contentWidth := int(float64(width) * messageBlockWidthRatio)

	// Apply styles to name and message text
	nameRendered := nameStyle.Render(name)
	messageRendered := textStyle.Render(text)

	// Apply width and alignment
	nameBlock := lipgloss.NewStyle().Width(contentWidth).Align(align).Render(nameRendered)
	messageBlock := lipgloss.NewStyle().Width(contentWidth).Align(align).Render(messageRendered)

	// Join vertically and wrap in container
	joined := lipgloss.JoinVertical(lipgloss.Left, nameBlock, messageBlock)

	content := lipgloss.NewStyle().
		Width(contentWidth).
		MarginTop(1).
		Render(joined)

	return lipgloss.PlaceHorizontal(width, align, content)
}

// Helper functions for rendering user and agent message blocks
func renderUserBlock(width int, nameStyle, bodyStyle lipgloss.Style, label string, body string) string {
	return renderMessageBlock(width, nameStyle, bodyStyle, strings.ToUpper(label), body, lipgloss.Right)
}

func renderAgentBlock(width int, nameStyle, bodyStyle lipgloss.Style, label string, body string) string {
	return renderMessageBlock(width, nameStyle, bodyStyle, strings.ToUpper(label), body, lipgloss.Left)
}
