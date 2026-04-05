package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"project-orb/internal/agent"
	"project-orb/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tokenMsg string

type streamErrMsg struct {
	err error
}

type spinnerTickMsg struct{}
type startWelcomeMsg struct{}

type streamDoneMsg struct {
	session  agent.SessionContext
	canceled bool
}

type tokenChannelClosedMsg struct{}
type errChannelClosedMsg struct{}
type doneChannelClosedMsg struct{}

type modelDependencies struct {
	runnerFactory runnerFactory
	currentMode   agent.Mode
	agentName     string
	personaPath   string
	err           error
	statusMessage string
}

type model struct {
	width                int
	height               int
	input                string
	pendingPrompt        string
	output               string
	statusMessage        string
	modeSelectorActive   bool
	modeSelectorIndex    int
	waitingForFirstToken bool
	spinnerFrame         int
	session              agent.SessionContext
	currentMode          agent.Mode
	agentName            string
	personaPath          string
	streaming            bool
	completed            bool
	cancelCurrent        context.CancelFunc
	shutdownCtx          context.Context
	err                  error
	tokenCh              <-chan string
	errCh                <-chan error
	doneCh               <-chan streamResult
	runner               streamRunner
	runnerFactory        runnerFactory
	inputBox             lipgloss.Style
	selectorBoxStyle     lipgloss.Style
	statusBarStyle       lipgloss.Style
	helpStyle            lipgloss.Style
	errorStyle           lipgloss.Style
	metaStyle            lipgloss.Style
	summaryTitleStyle    lipgloss.Style
	summaryBodyStyle     lipgloss.Style
	userNameStyle        lipgloss.Style
	agentNameStyle       lipgloss.Style
	userBodyStyle        lipgloss.Style
	agentBodyStyle       lipgloss.Style
}

func newModel(deps modelDependencies) model {
	if deps.currentMode.ID == "" {
		deps.currentMode = agent.DefaultMode()
	}
	if deps.agentName == "" {
		deps.agentName = "Agent"
	}

	m := model{
		statusMessage: deps.statusMessage,
		currentMode:   deps.currentMode,
		runnerFactory: deps.runnerFactory,
		agentName:     deps.agentName,
		personaPath:   deps.personaPath,
		err:           deps.err,
	}

	m.applyStyles(ui.NewStyles(deps.currentMode.ID))
	return m
}

func (m model) Init() tea.Cmd {
	if m.runnerFactory == nil || m.err != nil {
		return nil
	}

	return func() tea.Msg {
		return startWelcomeMsg{}
	}
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
		m.spinnerFrame = (m.spinnerFrame + 1) % len(thinkingText)
		return m, spinnerTick()
	case startWelcomeMsg:
		return m.startWelcome()
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
	if m.modeSelectorActive && !m.streaming {
		return m.handleModeSelectorKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		// Cancel any ongoing stream before quitting
		if m.streaming && m.cancelCurrent != nil {
			slog.Info("Canceling ongoing stream before shutdown...")
			m.cancelCurrent()
		}
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
		m.syncSlashCommandUI()
		return m, nil
	case tea.KeySpace:
		if m.streaming {
			return m, nil
		}
		m.input += " "
		m.syncSlashCommandUI()
		return m, nil
	default:
		if m.streaming {
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.syncSlashCommandUI()
		}
		return m, nil
	}
}

func (m model) handleModeSelectorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.currentModeMatches()

	switch msg.Type {
	case tea.KeyCtrlC:
		// Cancel any ongoing stream before quitting
		if m.streaming && m.cancelCurrent != nil {
			slog.Info("Canceling ongoing stream before shutdown...")
			m.cancelCurrent()
		}
		return m, tea.Quit
	case tea.KeyEsc:
		m.modeSelectorActive = false
		return m, nil
	case tea.KeyUp:
		if len(matches) > 0 && m.modeSelectorIndex > 0 {
			m.modeSelectorIndex--
		}
		return m, nil
	case tea.KeyDown:
		if len(matches) > 0 && m.modeSelectorIndex < len(matches)-1 {
			m.modeSelectorIndex++
		}
		return m, nil
	case tea.KeyEnter:
		return m.selectHighlightedMode()
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.input) == 0 {
			return m, nil
		}
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		m.resetModeSelectorIndex()
		if !m.isModeCommandInput() {
			m.modeSelectorActive = false
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		m.resetModeSelectorIndex()
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.resetModeSelectorIndex()
		}
		return m, nil
	}
}

func (m model) startPrompt() (tea.Model, tea.Cmd) {
	if m.streaming || strings.TrimSpace(m.input) == "" {
		return m, nil
	}

	prompt := strings.TrimSpace(m.input)
	if strings.HasPrefix(prompt, "/") {
		return m.handleCommand(prompt)
	}

	m.err = nil
	m.statusMessage = ""
	m.completed = false
	m.streaming = true
	m.waitingForFirstToken = true
	m.spinnerFrame = 0
	m.pendingPrompt = prompt
	m.output = ""
	m.input = ""

	runner, err := m.ensureRunner()
	if err != nil {
		m.streaming = false
		m.waitingForFirstToken = false
		m.err = err
		m.input = prompt
		m.pendingPrompt = ""
		return m, nil
	}

	cmd, tokenCh, errCh, doneCh, cancel := runner.Start(prompt, m.session)
	m.tokenCh = tokenCh
	m.errCh = errCh
	m.doneCh = doneCh
	m.cancelCurrent = cancel

	return m, tea.Batch(cmd, spinnerTick())
}

func (m model) startWelcome() (tea.Model, tea.Cmd) {
	if m.streaming || len(m.session.Recent) > 0 || strings.TrimSpace(m.session.Summary) != "" {
		return m, nil
	}

	runner, err := m.ensureRunner()
	if err != nil {
		m.err = err
		return m, nil
	}

	m.err = nil
	m.statusMessage = ""
	m.completed = false
	m.streaming = true
	m.waitingForFirstToken = true
	m.spinnerFrame = 0
	m.pendingPrompt = ""
	m.output = ""

	cmd, tokenCh, errCh, doneCh, cancel := runner.StartWelcome(m.session)
	m.tokenCh = tokenCh
	m.errCh = errCh
	m.doneCh = doneCh
	m.cancelCurrent = cancel

	return m, tea.Batch(cmd, spinnerTick())
}

func (m model) ensureRunner() (streamRunner, error) {
	if m.runner != nil {
		return m.runner, nil
	}

	if m.runnerFactory == nil {
		return nil, errRunnerNotConfigured
	}

	runner, err := m.runnerFactory(m.currentMode)
	if err != nil {
		return nil, err
	}

	m.runner = runner
	return m.runner, nil
}

func (m model) handleCommand(command string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return m, nil
	}

	switch fields[0] {
	case "/mode", "/modes":
		return m.handleModeCommand(fields)
	default:
		m.err = fmt.Errorf("unknown command %q", fields[0])
		m.statusMessage = ""
		return m, nil
	}
}

func (m model) handleModeCommand(_ []string) (tea.Model, tea.Cmd) {
	m.err = nil
	m.completed = false

	if !m.modeSelectorActive {
		m.modeSelectorActive = true
		m.statusMessage = ""
		m.resetModeSelectorIndex()
		return m, nil
	}

	if len(m.currentModeMatches()) == 0 {
		m.err = fmt.Errorf("no matching mode for %q", modeQueryFromInput(m.input))
		m.statusMessage = ""
		return m, nil
	}

	return m.selectHighlightedMode()
}

func (m model) selectHighlightedMode() (tea.Model, tea.Cmd) {
	matches := m.currentModeMatches()
	if len(matches) == 0 {
		m.err = fmt.Errorf("no matching mode for %q", modeQueryFromInput(m.input))
		m.statusMessage = ""
		return m, nil
	}

	mode := matches[m.modeSelectorIndex]
	if mode.ID == m.currentMode.ID {
		m.modeSelectorActive = false
		m.input = ""
		return m, nil
	}

	if m.runnerFactory == nil {
		m.err = errRunnerNotConfigured
		m.statusMessage = ""
		return m, nil
	}

	runner, err := m.runnerFactory(mode)
	if err != nil {
		m.err = err
		m.statusMessage = ""
		return m, nil
	}

	m.switchToMode(mode, runner)
	return m, tea.Cmd(func() tea.Msg { return startWelcomeMsg{} })
}

func (m *model) switchToMode(mode agent.Mode, runner streamRunner) {
	// Cancel any ongoing stream before switching
	if m.streaming && m.cancelCurrent != nil {
		m.cancelCurrent()
		m.cancelCurrent = nil
	}

	m.input = ""
	m.modeSelectorActive = false
	m.modeSelectorIndex = 0
	m.currentMode = mode
	m.runner = runner
	m.session = agent.SessionContext{}
	m.pendingPrompt = ""
	m.output = ""
	m.streaming = false
	m.waitingForFirstToken = false
	m.tokenCh = nil
	m.errCh = nil
	m.doneCh = nil
	m.statusMessage = fmt.Sprintf("Switched to %s mode. Started a fresh conversation.", mode.Name)

	m.applyStyles(ui.NewStyles(mode.ID))
	slog.Info("Switched mode", "mode", mode.Name, "source", "User")
}

func (m *model) applyStyles(s ui.Styles) {
	m.inputBox = s.InputBox
	m.selectorBoxStyle = s.SelectorBoxStyle
	m.statusBarStyle = s.StatusBarStyle
	m.helpStyle = s.HelpStyle
	m.errorStyle = s.ErrorStyle
	m.metaStyle = s.MetaStyle
	m.summaryTitleStyle = s.SummaryTitleStyle
	m.summaryBodyStyle = s.SummaryBodyStyle
	m.userNameStyle = s.UserNameStyle
	m.agentNameStyle = s.CoachNameStyle
	m.userBodyStyle = s.UserBodyStyle
	m.agentBodyStyle = s.CoachBodyStyle
}

func (m model) handleStreamError(err error) (tea.Model, tea.Cmd) {
	m.streaming = false
	m.waitingForFirstToken = false
	m.cancelCurrent = nil
	m.completed = false
	m.err = err
	m.statusMessage = ""
	slog.Error("Stream error", "err", err, "source", "System")
	return m, nil
}

func (m model) currentModeMatches() []agent.Mode {
	return matchingModes(modeQueryFromInput(m.input))
}

func (m *model) resetModeSelectorIndex() {
	matches := m.currentModeMatches()
	if len(matches) == 0 {
		m.modeSelectorIndex = 0
		return
	}

	for i, mode := range matches {
		if mode.ID == m.currentMode.ID {
			m.modeSelectorIndex = i
			return
		}
	}

	m.modeSelectorIndex = 0
}

func (m *model) syncSlashCommandUI() {
	if !m.isModeCommandInput() {
		m.modeSelectorActive = false
		m.modeSelectorIndex = 0
		return
	}

	m.modeSelectorActive = true
	m.resetModeSelectorIndex()
}

func (m model) isModeCommandInput() bool {
	trimmed := strings.TrimSpace(m.input)
	return strings.HasPrefix(trimmed, "/mode") || strings.HasPrefix(trimmed, "/modes")
}

func (m model) handleStreamDone(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	m.streaming = false
	m.waitingForFirstToken = false
	m.cancelCurrent = nil
	m.completed = !msg.canceled
	m.session = msg.session
	m.err = nil

	hasOutput := strings.TrimSpace(m.output) != ""

	if msg.canceled && !hasOutput && m.pendingPrompt != "" {
		m.input = m.pendingPrompt
	} else if hasOutput {
		m.session.AddTurn(agent.Turn{
			User:      m.pendingPrompt,
			Assistant: m.output,
		})
	}

	m.pendingPrompt = ""
	m.output = ""

	return m, nil
}
