package main

import (
	"context"
	"strings"

	"project-orb/internal/coach"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tokenMsg string

type streamErrMsg struct {
	err error
}

type spinnerTickMsg struct{}

type streamDoneMsg struct {
	session  coach.SessionContext
	canceled bool
}

type tokenChannelClosedMsg struct{}
type errChannelClosedMsg struct{}
type doneChannelClosedMsg struct{}

type modelDependencies struct {
	runner      streamRunner
	coachName   string
	personaPath string
	err         error
}

type model struct {
	width                int
	height               int
	input                string
	pendingPrompt        string
	output               string
	waitingForFirstToken bool
	spinnerFrame         int
	session              coach.SessionContext
	coachName            string
	personaPath          string
	streaming            bool
	completed            bool
	cancelCurrent        context.CancelFunc
	err                  error
	tokenCh              <-chan string
	errCh                <-chan error
	doneCh               <-chan streamResult
	runner               streamRunner
	inputBox             lipgloss.Style
	helpStyle            lipgloss.Style
	errorStyle           lipgloss.Style
	metaStyle            lipgloss.Style
	summaryTitleStyle    lipgloss.Style
	summaryBodyStyle     lipgloss.Style
	userNameStyle        lipgloss.Style
	auraNameStyle        lipgloss.Style
	userBodyStyle        lipgloss.Style
	auraBodyStyle        lipgloss.Style
}

func newModel(deps modelDependencies) model {
	if deps.coachName == "" {
		deps.coachName = "Coach"
	}

	styles := newStyles()

	return model{
		runner:            deps.runner,
		coachName:         deps.coachName,
		personaPath:       deps.personaPath,
		err:               deps.err,
		inputBox:          styles.inputBox,
		helpStyle:         styles.helpStyle,
		errorStyle:        styles.errorStyle,
		metaStyle:         styles.metaStyle,
		summaryTitleStyle: styles.summaryTitleStyle,
		summaryBodyStyle:  styles.summaryBodyStyle,
		userNameStyle:     styles.userNameStyle,
		auraNameStyle:     styles.auraNameStyle,
		userBodyStyle:     styles.userBodyStyle,
		auraBodyStyle:     styles.auraBodyStyle,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case spinnerTickMsg:
		if !m.streaming {
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tokenMsg:
		m.waitingForFirstToken = false
		m.output += string(msg)
		return m, waitForToken(m.tokenCh)
	case streamErrMsg:
		return m.handleStreamError(msg.err)
	case streamDoneMsg:
		return m.handleStreamDone(msg)
	case tokenChannelClosedMsg, errChannelClosedMsg, doneChannelClosedMsg:
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if m.streaming && m.cancelCurrent != nil {
			m.cancelCurrent()
		}
		return m, nil
	case tea.KeyEnter:
		return m.startPrompt()
	case tea.KeyBackspace, tea.KeyDelete:
		if m.streaming || len(m.input) == 0 {
			return m, nil
		}
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		return m, nil
	case tea.KeySpace:
		if m.streaming {
			return m, nil
		}
		m.input += " "
		return m, nil
	default:
		if m.streaming {
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		}
		return m, nil
	}
}

func (m model) startPrompt() (tea.Model, tea.Cmd) {
	if m.streaming || strings.TrimSpace(m.input) == "" {
		return m, nil
	}

	prompt := strings.TrimSpace(m.input)
	m.err = nil
	m.completed = false
	m.streaming = true
	m.waitingForFirstToken = true
	m.spinnerFrame = 0
	m.pendingPrompt = prompt
	m.output = ""
	m.input = ""

	if m.runner == nil {
		m.streaming = false
		m.waitingForFirstToken = false
		m.err = errRunnerNotConfigured
		m.input = prompt
		m.pendingPrompt = ""
		return m, nil
	}

	cmd, tokenCh, errCh, doneCh, cancel := m.runner.Start(prompt, m.session)
	m.tokenCh = tokenCh
	m.errCh = errCh
	m.doneCh = doneCh
	m.cancelCurrent = cancel

	return m, tea.Batch(cmd, spinnerTick())
}

func (m model) handleStreamError(err error) (tea.Model, tea.Cmd) {
	m.streaming = false
	m.waitingForFirstToken = false
	m.cancelCurrent = nil
	m.completed = false
	m.err = err
	return m, nil
}

func (m model) handleStreamDone(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	m.streaming = false
	m.waitingForFirstToken = false
	m.cancelCurrent = nil
	m.completed = !msg.canceled
	m.session = msg.session

	if msg.canceled {
		if strings.TrimSpace(m.output) == "" {
			m.input = m.pendingPrompt
		} else {
			m.session.AddTurn(coach.Turn{
				User:      m.pendingPrompt,
				Assistant: m.output,
			})
		}
	} else {
		m.session.AddTurn(coach.Turn{
			User:      m.pendingPrompt,
			Assistant: m.output,
		})
	}

	m.pendingPrompt = ""
	m.output = ""

	return m, nil
}
