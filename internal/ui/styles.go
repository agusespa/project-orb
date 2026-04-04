package ui

import (
	"project-orb/internal/agent"

	"github.com/charmbracelet/lipgloss"
)

const InputCursor = "█"

const (
	ColorSubdued  = "240"
	ColorMuted    = "245"
	ColorBright   = "250"
	ColorText     = "252"
	ColorEmphasis = "255"
	ColorError    = "203"

	ColorSuccess = "10"
	ColorWarning = "11"
	ColorCaution = "214"
	ColorDanger  = "9"
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

var NeutralSelectorBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color(ColorSubdued)).
	Padding(0, 1)

var NeutralHelpStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(ColorMuted))

var NeutralMetaStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(ColorMuted))

var NeutralSelectorTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color(ColorBright))

var SelectorModeNameStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(ColorText))

var SelectorModeNameHighlightStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(ColorEmphasis)).
	Bold(true)

var SelectorDescriptionStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color(ColorSubdued)).
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
		return ModeTheme{
			Border:         lipgloss.Color("172"),
			StatusFg:       lipgloss.Color("214"),
			SummaryTitleFg: lipgloss.Color("214"),
			SummaryBodyFg:  lipgloss.Color("180"),
			UserNameFg:     lipgloss.Color("220"),
			CoachNameFg:    lipgloss.Color("208"),
		}
	case agent.ModeCoach:
		return ModeTheme{
			Border:         lipgloss.Color("71"),
			StatusFg:       lipgloss.Color("114"),
			SummaryTitleFg: lipgloss.Color("114"),
			SummaryBodyFg:  lipgloss.Color("108"),
			UserNameFg:     lipgloss.Color("120"),
			CoachNameFg:    lipgloss.Color("71"),
		}
	default:
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
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(t.Border).
			Padding(0, 1),
		SelectorBoxStyle: NeutralSelectorBoxStyle,
		StatusBarStyle: lipgloss.NewStyle().
			Foreground(t.StatusFg).
			Bold(true),
		HelpStyle: NeutralHelpStyle,
		ErrorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorError)).
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
			Foreground(lipgloss.Color(ColorText)),
		CoachBodyStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorEmphasis)),
	}
}
