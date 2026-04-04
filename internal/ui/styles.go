package ui

import (
	"project-orb/internal/agent"

	"github.com/charmbracelet/lipgloss"
)

type Styles struct {
	InputBox          lipgloss.Style
	SelectorBoxStyle  lipgloss.Style
	StatusBarStyle    lipgloss.Style
	HelpStyle         lipgloss.Style
	ErrorStyle        lipgloss.Style
	MetaStyle         lipgloss.Style
	SummaryTitleStyle lipgloss.Style
	SummaryBodyStyle  lipgloss.Style
	UserNameStyle     lipgloss.Style
	CoachNameStyle    lipgloss.Style
	UserBodyStyle     lipgloss.Style
	CoachBodyStyle    lipgloss.Style
}

// Neutral styles for UI chrome (selector, footer)
var NeutralSelectorBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240")).
	Padding(0, 1)

var NeutralHelpStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("245"))

var NeutralMetaStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("245"))

var NeutralSelectorTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("250"))

var SelectorModeNameStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("252"))

var SelectorModeNameHighlightStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("255")).
	Bold(true)

var SelectorDescriptionStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("240")).
	Italic(true)

type ModeTheme struct {
	Border         lipgloss.Color
	StatusFg       lipgloss.Color
	SummaryTitleFg lipgloss.Color
	SummaryBodyFg  lipgloss.Color
	UserNameFg     lipgloss.Color
	CoachNameFg    lipgloss.Color
}

func ThemeForMode(id agent.ModeID) ModeTheme {
	switch id {
	case agent.ModePerformanceReview:
		// Dynamic, energetic: orange / yellow tones
		return ModeTheme{
			Border:         lipgloss.Color("172"),
			StatusFg:       lipgloss.Color("214"),
			SummaryTitleFg: lipgloss.Color("214"),
			SummaryBodyFg:  lipgloss.Color("180"),
			UserNameFg:     lipgloss.Color("220"),
			CoachNameFg:    lipgloss.Color("208"),
		}
	case agent.ModeCoach:
		// Grounded, growing: green tones
		return ModeTheme{
			Border:         lipgloss.Color("71"),
			StatusFg:       lipgloss.Color("114"),
			SummaryTitleFg: lipgloss.Color("114"),
			SummaryBodyFg:  lipgloss.Color("108"),
			UserNameFg:     lipgloss.Color("120"),
			CoachNameFg:    lipgloss.Color("71"),
		}
	default:
		// Analyst: cerebral blue/purple tones (original palette)
		return ModeTheme{
			Border:         lipgloss.Color("61"),
			StatusFg:       lipgloss.Color("147"),
			SummaryTitleFg: lipgloss.Color("147"),
			SummaryBodyFg:  lipgloss.Color("146"),
			UserNameFg:     lipgloss.Color("111"),
			CoachNameFg:    lipgloss.Color("97"),
		}
	}
}

func NewStyles(id agent.ModeID) Styles {
	t := ThemeForMode(id)
	return Styles{
		InputBox: lipgloss.NewStyle().
			BorderTop(true).
			BorderBottom(true).
			BorderLeft(false).
			BorderRight(false).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(t.Border).
			Padding(0, 1),
		SelectorBoxStyle: NeutralSelectorBoxStyle,
		StatusBarStyle: lipgloss.NewStyle().
			Foreground(t.StatusFg).
			Bold(true),
		HelpStyle: NeutralHelpStyle,
		ErrorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		MetaStyle: NeutralMetaStyle,
		SummaryTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.SummaryTitleFg),
		SummaryBodyStyle: lipgloss.NewStyle().
			Foreground(t.SummaryBodyFg),
		UserNameStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.UserNameFg),
		CoachNameStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.CoachNameFg),
		UserBodyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),
		CoachBodyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")),
	}
}
