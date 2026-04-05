package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"project-orb/internal/agent"

	tea "github.com/charmbracelet/bubbletea"
)

type tokenMsg string

type streamErrMsg struct {
	err error
}

type spinnerTickMsg struct{}

type streamDoneMsg struct {
	session  agent.SessionContext
	canceled bool
}

type tokenChannelClosedMsg struct{}
type errChannelClosedMsg struct{}
type doneChannelClosedMsg struct{}

type ModelDependencies struct {
	RunnerFactory RunnerFactory
	CurrentMode   agent.Mode
	AgentName     string
	PersonaPath   string
	StatusMessage string
}

type streamState struct {
	active               bool
	completed            bool
	waitingForFirstToken bool
	spinnerFrame         int
	cancelCurrent        context.CancelFunc
	tokenCh              <-chan string
	errCh                <-chan error
	doneCh               <-chan StreamResult
	runner               StreamRunner
}

type modeSelector struct {
	active bool
	index  int
}

type Model struct {
	width  int
	height int

	input         string
	pendingPrompt string
	output        string
	statusMessage string

	modeSelector modeSelector

	stream streamState

	session       agent.SessionContext
	currentMode   agent.Mode
	agentName     string
	personaPath   string
	runnerFactory RunnerFactory

	shutdownCtx context.Context
	err         error

	styles Styles
}

func (m *Model) SetShutdownCtx(ctx context.Context) {
	m.shutdownCtx = ctx
}

func NewModel(deps ModelDependencies) Model {
	if deps.CurrentMode.ID == "" {
		deps.CurrentMode = agent.DefaultMode()
	}
	if deps.AgentName == "" {
		deps.AgentName = "Coach"
	}

	return Model{
		statusMessage: deps.StatusMessage,
		currentMode:   deps.CurrentMode,
		runnerFactory: deps.RunnerFactory,
		agentName:     deps.AgentName,
		personaPath:   deps.PersonaPath,
		styles:        NewStyles(deps.CurrentMode.ID),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case spinnerTickMsg:
		if !m.stream.active {
			return m, nil
		}
		m.stream.spinnerFrame = (m.stream.spinnerFrame + 1) % len(thinkingText)
		return m, spinnerTick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tokenMsg:
		m.stream.waitingForFirstToken = false
		m.output += string(msg)
		return m, waitForToken(m.stream.tokenCh)
	case streamErrMsg:
		return m.handleStreamError(msg.err)
	case streamDoneMsg:
		return m.handleStreamDone(msg)
	case tokenChannelClosedMsg, errChannelClosedMsg, doneChannelClosedMsg:
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modeSelector.active && !m.stream.active {
		return m.handleModeSelectorKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.stream.active && m.stream.cancelCurrent != nil {
			slog.Info("Canceling ongoing stream before shutdown...")
			m.stream.cancelCurrent()
		}
		return m, tea.Quit
	case tea.KeyEsc:
		if m.stream.active && m.stream.cancelCurrent != nil {
			m.stream.cancelCurrent()
		}
		return m, nil
	case tea.KeyEnter:
		return m.startPrompt()
	case tea.KeyBackspace, tea.KeyDelete:
		if m.stream.active || len(m.input) == 0 {
			return m, nil
		}
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		m.syncSlashCommandUI()
		return m, nil
	case tea.KeySpace:
		if m.stream.active {
			return m, nil
		}
		m.input += " "
		m.syncSlashCommandUI()
		return m, nil
	default:
		if m.stream.active {
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.syncSlashCommandUI()
		}
		return m, nil
	}
}

func (m Model) handleModeSelectorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.currentModeMatches()

	switch msg.Type {
	case tea.KeyCtrlC:
		if m.stream.active && m.stream.cancelCurrent != nil {
			slog.Info("Canceling ongoing stream before shutdown...")
			m.stream.cancelCurrent()
		}
		return m, tea.Quit
	case tea.KeyEsc:
		m.modeSelector.active = false
		return m, nil
	case tea.KeyUp:
		if len(matches) > 0 && m.modeSelector.index > 0 {
			m.modeSelector.index--
		}
		return m, nil
	case tea.KeyDown:
		if len(matches) > 0 && m.modeSelector.index < len(matches)-1 {
			m.modeSelector.index++
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
			m.modeSelector.active = false
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

func (m Model) startPrompt() (tea.Model, tea.Cmd) {
	if m.stream.active || strings.TrimSpace(m.input) == "" {
		return m, nil
	}

	prompt := strings.TrimSpace(m.input)
	if strings.HasPrefix(prompt, "/") {
		return m.handleCommand(prompt)
	}

	m.err = nil
	m.statusMessage = ""
	m.stream.completed = false
	m.stream.active = true
	m.stream.waitingForFirstToken = true
	m.stream.spinnerFrame = 0
	m.pendingPrompt = prompt
	m.output = ""
	m.input = ""

	runner, err := m.ensureRunner()
	if err != nil {
		m.stream.active = false
		m.stream.waitingForFirstToken = false
		m.err = err
		m.input = prompt
		m.pendingPrompt = ""
		return m, nil
	}

	cmd, channels := runner.Start(prompt, m.session)
	m.stream.tokenCh = channels.TokenCh
	m.stream.errCh = channels.ErrCh
	m.stream.doneCh = channels.DoneCh
	m.stream.cancelCurrent = channels.Cancel

	return m, tea.Batch(cmd, spinnerTick())
}

func (m Model) ensureRunner() (StreamRunner, error) {
	if m.stream.runner != nil {
		return m.stream.runner, nil
	}

	if m.runnerFactory == nil {
		return nil, ErrRunnerNotConfigured
	}

	runner, err := m.runnerFactory(m.currentMode)
	if err != nil {
		return nil, err
	}

	m.stream.runner = runner
	return m.stream.runner, nil
}

func (m Model) handleCommand(command string) (tea.Model, tea.Cmd) {
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

func (m Model) handleModeCommand(_ []string) (tea.Model, tea.Cmd) {
	m.err = nil
	m.stream.completed = false

	if !m.modeSelector.active {
		m.modeSelector.active = true
		m.statusMessage = ""
		m.resetModeSelectorIndex()
		return m, nil
	}

	if len(m.currentModeMatches()) == 0 {
		m.err = fmt.Errorf("no matching mode for %q", ModeQueryFromInput(m.input))
		m.statusMessage = ""
		return m, nil
	}

	return m.selectHighlightedMode()
}

func (m Model) selectHighlightedMode() (tea.Model, tea.Cmd) {
	matches := m.currentModeMatches()
	if len(matches) == 0 {
		m.err = fmt.Errorf("no matching mode for %q", ModeQueryFromInput(m.input))
		m.statusMessage = ""
		return m, nil
	}

	mode := matches[m.modeSelector.index]
	if mode.ID == m.currentMode.ID {
		m.modeSelector.active = false
		m.input = ""
		return m, nil
	}

	if m.runnerFactory == nil {
		m.err = ErrRunnerNotConfigured
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
	return m, nil
}

func (m *Model) switchToMode(mode agent.Mode, runner StreamRunner) {
	if m.stream.active && m.stream.cancelCurrent != nil {
		m.stream.cancelCurrent()
		m.stream.cancelCurrent = nil
	}

	m.input = ""
	m.modeSelector.active = false
	m.modeSelector.index = 0
	m.currentMode = mode
	m.stream.runner = runner
	m.session = agent.SessionContext{}
	m.pendingPrompt = ""
	m.output = ""
	m.stream.active = false
	m.stream.waitingForFirstToken = false
	m.stream.tokenCh = nil
	m.stream.errCh = nil
	m.stream.doneCh = nil
	m.statusMessage = fmt.Sprintf("Switched to %s mode. Started a fresh conversation.", mode.Name)
	m.styles = NewStyles(mode.ID)

	slog.Info("Switched mode", "mode", mode.Name, "source", "User")
}

func (m Model) handleStreamError(err error) (tea.Model, tea.Cmd) {
	m.stream.active = false
	m.stream.waitingForFirstToken = false
	m.stream.cancelCurrent = nil
	m.stream.completed = false
	m.err = err
	m.statusMessage = ""
	slog.Error("Stream error", "err", err, "source", "System")
	return m, nil
}

func (m Model) currentModeMatches() []agent.Mode {
	return MatchingModes(ModeQueryFromInput(m.input))
}

func (m *Model) resetModeSelectorIndex() {
	matches := m.currentModeMatches()
	if len(matches) == 0 {
		m.modeSelector.index = 0
		return
	}

	for i, mode := range matches {
		if mode.ID == m.currentMode.ID {
			m.modeSelector.index = i
			return
		}
	}

	m.modeSelector.index = 0
}

func (m *Model) syncSlashCommandUI() {
	if !m.isModeCommandInput() {
		m.modeSelector.active = false
		m.modeSelector.index = 0
		return
	}

	m.modeSelector.active = true
	m.resetModeSelectorIndex()
}

func (m Model) isModeCommandInput() bool {
	trimmed := strings.TrimSpace(m.input)
	return strings.HasPrefix(trimmed, "/mode") || strings.HasPrefix(trimmed, "/modes")
}

func (m Model) handleStreamDone(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	m.stream.active = false
	m.stream.waitingForFirstToken = false
	m.stream.cancelCurrent = nil
	m.stream.completed = !msg.canceled
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
