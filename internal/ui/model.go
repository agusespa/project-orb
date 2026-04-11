package ui

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"project-orb/internal/agent"
	"project-orb/internal/text"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type tokenMsg string

type streamErrMsg struct {
	err error
}

type spinnerTickMsg struct{}

type tokenChannelClosedMsg struct{}
type errChannelClosedMsg struct{}
type doneChannelClosedMsg struct{}
type shutdownCompleteMsg struct {
	err error
}

type startupMessagesLoadedMsg struct {
	messages []string
	err      error
}

type analystSecondMessageLoadedMsg struct {
	message string
	err     error
}

type ModelDependencies struct {
	Client         *agent.Client
	CurrentMode    agent.Mode
	AgentName      string
	InitialSession agent.SessionContext
	SessionStore   agent.SessionStore
	StatusMessage  string
	Shutdown       func() error
}

type streamState struct {
	active               bool
	completed            bool
	waitingForFirstToken bool
	spinnerFrame         int
	spinnerText          string
	cancelCurrent        context.CancelFunc
	tokenCh              <-chan string
	errCh                <-chan error
	doneCh               <-chan agent.StreamResult
	runner               *AgentRunner
}

type modeSelector struct {
	active bool
	index  int
}

type hintsOverlay struct {
	active bool
}

type Model struct {
	width  int
	height int

	input           string
	pendingPrompt   string
	output          string
	statusMessage   string
	startupMessages []string
	pendingQuit     bool
	pendingSwitch   agent.ModeID

	modeSelector modeSelector
	hintsOverlay hintsOverlay
	viewport     ChatViewport

	stream streamState

	session      agent.SessionContext
	currentMode  agent.Mode
	agentName    string
	client       *agent.Client
	sessionStore agent.SessionStore

	shutdownCtx  context.Context
	shutdownFn   func() error
	shuttingDown bool
	err          error

	// Loading state for analyst second message
	loadingAnalystMessage bool
	analystLoadingFrame   int

	styles Styles
}

func (m *Model) updateViewportContent() {
	if !m.viewport.Ready() {
		return
	}

	contentWidth := ChatContentWidth(m.width)
	chatContent := m.renderChatContent(contentWidth)
	m.viewport.SetContent(ansi.Wrap(chatContent, contentWidth, ""))
}

func (m *Model) SetShutdownCtx(ctx context.Context) {
	m.shutdownCtx = ctx
}

func NewModel(deps ModelDependencies) (Model, error) {
	if deps.CurrentMode.ID == "" {
		deps.CurrentMode = agent.DefaultMode()
	}
	if deps.AgentName == "" {
		deps.AgentName = "Coach"
	}

	model := Model{
		statusMessage: deps.StatusMessage,
		currentMode:   deps.CurrentMode,
		session:       deps.InitialSession,
		client:        deps.Client,
		agentName:     deps.AgentName,
		sessionStore:  deps.SessionStore,
		shutdownFn:    deps.Shutdown,
		styles:        NewStyles(deps.CurrentMode.ID),
	}

	return model, nil
}

func (m Model) Init() tea.Cmd {
	return m.loadStartupMessages()
}

func (m Model) loadStartupMessages() tea.Cmd {
	return func() tea.Msg {
		messages, err := m.startupMessagesForSession(context.Background())
		return startupMessagesLoadedMsg{
			messages: messages,
			err:      err,
		}
	}
}

func (m Model) loadAnalystSecondMessage() tea.Cmd {
	return func() tea.Msg {
		runner, err := m.ensureRunner()
		if err != nil {
			return analystSecondMessageLoadedMsg{err: err}
		}

		message, err := runner.Service.LoadAnalystSecondMessage(context.Background(), m.session)
		return analystSecondMessageLoadedMsg{
			message: message,
			err:     err,
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate viewport dimensions
		selectorHeight := 0
		if m.modeSelector.active {
			// Estimate selector height
			selectorHeight = min(maxModeSelectorLines+1, len(m.currentModeMatches())+2)
		} else if m.hintsOverlay.active {
			selectorHeight = min(maxHintsOverlayLines, len(hintsOverlayLines())+2)
		}

		m.viewport.Resize(m.width, m.height, selectorHeight)
		m.updateViewportContent()

		return m, nil
	case spinnerTickMsg:
		if !m.stream.active && !m.loadingAnalystMessage {
			return m, nil
		}
		if m.stream.active {
			textLen := len([]rune(m.stream.spinnerText))
			if textLen > 0 {
				m.stream.spinnerFrame = (m.stream.spinnerFrame + 1) % textLen
			}
		}
		if m.loadingAnalystMessage {
			textLen := len([]rune(LoadingMemoryText))
			if textLen > 0 {
				m.analystLoadingFrame = (m.analystLoadingFrame + 1) % textLen
			}
		}
		// Update viewport to show spinner animation
		m.updateViewportContent()
		return m, spinnerTick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case startupMessagesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.updateViewportContent()
			return m, nil
		}
		m.startupMessages = msg.messages
		m.updateViewportContent()

		// For analyst mode, load the second message asynchronously
		if m.currentMode.ID == agent.ModeAnalyst {
			m.loadingAnalystMessage = true
			m.analystLoadingFrame = 0
			return m, tea.Batch(m.loadAnalystSecondMessage(), spinnerTick())
		}
		return m, nil
	case analystSecondMessageLoadedMsg:
		m.loadingAnalystMessage = false
		if msg.err != nil {
			m.err = msg.err
			m.updateViewportContent()
			return m, nil
		}
		if strings.TrimSpace(msg.message) != "" {
			m.startupMessages = append(m.startupMessages, msg.message)
			m.updateViewportContent()
		}
		return m, nil
	case tokenMsg:
		m.stream.waitingForFirstToken = false
		m.output += string(msg)
		// Update viewport content and scroll to bottom when new content arrives
		m.updateViewportContent()
		m.viewport.GotoBottom()
		return m, waitForToken(m.stream.tokenCh)
	case streamErrMsg:
		return m.handleStreamError(msg.err)
	case agent.StreamResult:
		return m.handleStreamDone(msg)
	case tokenChannelClosedMsg, errChannelClosedMsg, doneChannelClosedMsg:
		return m, nil
	case shutdownCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			m.shuttingDown = false
			m.pendingQuit = true
			m.statusMessage = text.ShutdownFailed
			return m, nil
		}
		return m, tea.Quit
	}

	// Update viewport for scrolling
	cmd = m.viewport.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modeSelector.active && !m.stream.active && !m.loadingAnalystMessage {
		return m.handleModeSelectorKey(msg)
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		return m.handleCtrlC()
	case tea.KeyEsc:
		if m.hintsOverlay.active {
			m.hintsOverlay.active = false
			return m, nil
		}
		m.clearDiscardWarnings()
		if m.stream.active && m.stream.cancelCurrent != nil {
			m.stream.cancelCurrent()
		}
		return m, nil
	case tea.KeyEnter:
		return m.startPrompt()
	case tea.KeyBackspace, tea.KeyDelete:
		if m.stream.active || m.loadingAnalystMessage || len(m.input) == 0 {
			return m, nil
		}
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		m.clearDiscardWarnings()
		m.syncSlashCommandUI()
		return m, nil
	case tea.KeySpace:
		if m.stream.active || m.loadingAnalystMessage {
			return m, nil
		}
		m.input += " "
		m.clearDiscardWarnings()
		m.syncSlashCommandUI()
		return m, nil
	default:
		if m.stream.active || m.loadingAnalystMessage {
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.clearDiscardWarnings()
			m.syncSlashCommandUI()
		}
		return m, nil
	}
}

func (m Model) handleModeSelectorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.currentModeMatches()

	switch msg.Type {
	case tea.KeyCtrlC:
		return m.handleCtrlC()
	case tea.KeyEsc:
		m.clearDiscardWarnings()
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
		m.clearDiscardWarnings()
		m.resetModeSelectorIndex()
		if !m.isModeCommandInput() {
			m.modeSelector.active = false
		}
		return m, nil
	case tea.KeySpace:
		m.input += " "
		m.clearDiscardWarnings()
		m.resetModeSelectorIndex()
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.clearDiscardWarnings()
			m.resetModeSelectorIndex()
		}
		return m, nil
	}
}

func (m Model) startPrompt() (tea.Model, tea.Cmd) {
	if m.stream.active || m.loadingAnalystMessage || strings.TrimSpace(m.input) == "" {
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
	m.stream.spinnerText = ThinkingText
	m.pendingPrompt = prompt
	m.output = ""
	m.input = ""

	// Update viewport content and scroll to bottom when starting a new prompt
	m.updateViewportContent()
	m.viewport.GotoBottom()

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

func (m Model) ensureRunner() (*AgentRunner, error) {
	if m.stream.runner != nil {
		return m.stream.runner, nil
	}

	if m.client == nil {
		return nil, agent.ErrRunnerNotConfigured
	}

	service, err := agent.NewService(m.client, m.currentMode)
	if err != nil {
		return nil, err
	}
	service.SetSessionStore(m.sessionStore)

	m.stream.runner = &AgentRunner{Service: service}
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
	case "/hints":
		return m.handleHintsCommand()
	case "/wrap":
		return m.handleWrapCommand()
	default:
		m.err = errors.New(text.UnknownCommand(fields[0]))
		m.statusMessage = ""
		return m, nil
	}
}

func (m Model) handleHintsCommand() (tea.Model, tea.Cmd) {
	m.err = nil
	m.stream.completed = false
	m.hintsOverlay.active = true
	m.modeSelector.active = false
	m.input = ""
	return m, nil
}

func (m Model) handleModeCommand(_ []string) (tea.Model, tea.Cmd) {
	m.err = nil
	m.stream.completed = false
	m.hintsOverlay.active = false

	if !m.modeSelector.active {
		m.modeSelector.active = true
		m.statusMessage = ""
		m.resetModeSelectorIndex()
		return m, nil
	}

	if len(m.currentModeMatches()) == 0 {
		m.err = errors.New(text.NoMatchingMode(ModeQueryFromInput(m.input)))
		m.statusMessage = ""
		return m, nil
	}

	return m.selectHighlightedMode()
}

func (m Model) handleWrapCommand() (tea.Model, tea.Cmd) {
	m.err = nil
	m.stream.completed = false

	if m.stream.active {
		m.err = errors.New(text.CannotWrapWhileStreaming)
		m.statusMessage = ""
		return m, nil
	}

	if err := m.persistCurrentSession(context.Background()); err != nil {
		m.err = err
		m.statusMessage = ""
		return m, nil
	}

	m.clearDiscardWarnings()
	m.statusMessage = text.SessionWrapped(m.currentMode.Name)
	return m, tea.Quit
}

func (m Model) selectHighlightedMode() (tea.Model, tea.Cmd) {
	matches := m.currentModeMatches()
	if len(matches) == 0 {
		m.err = errors.New(text.NoMatchingMode(ModeQueryFromInput(m.input)))
		m.statusMessage = ""
		return m, nil
	}

	mode := matches[m.modeSelector.index]
	if mode.ID == m.currentMode.ID {
		m.modeSelector.active = false
		m.input = ""
		return m, nil
	}

	if m.client == nil {
		m.err = agent.ErrRunnerNotConfigured
		m.statusMessage = ""
		return m, nil
	}

	if m.shouldWarnAboutDiscard() && m.pendingSwitch != mode.ID {
		m.pendingQuit = false
		m.pendingSwitch = mode.ID
		m.err = nil
		m.statusMessage = text.UnsavedSessionSwitchWarning(mode.Name)
		return m, nil
	}

	service, err := agent.NewService(m.client, mode)
	if err != nil {
		m.err = err
		m.statusMessage = ""
		return m, nil
	}
	service.SetSessionStore(m.sessionStore)

	// Load startup messages BEFORE switching mode to avoid flash
	var session agent.SessionContext
	if m.sessionStore != nil && mode.ID == agent.ModeAnalyst {
		session, _, err = service.LoadSession(context.Background())
		if err != nil {
			m.err = err
			m.statusMessage = ""
			return m, nil
		}
	} else {
		session = agent.NewSessionContext()
	}

	startupMessages, err := service.StartupMessages(context.Background(), session)
	if err != nil {
		m.err = err
		m.statusMessage = ""
		return m, nil
	}

	runner := &AgentRunner{Service: service}
	m.switchToMode(mode, runner, session, startupMessages)
	return m, nil
}

func (m *Model) switchToMode(mode agent.Mode, runner *AgentRunner, session agent.SessionContext, startupMessages []string) {
	if m.stream.active && m.stream.cancelCurrent != nil {
		m.stream.cancelCurrent()
		m.stream.cancelCurrent = nil
	}

	m.input = ""
	m.modeSelector.active = false
	m.modeSelector.index = 0
	m.hintsOverlay.active = false
	m.currentMode = mode
	m.stream.runner = runner
	m.session = session
	m.pendingPrompt = ""
	m.output = ""
	m.clearDiscardWarnings()
	m.stream.active = false
	m.stream.waitingForFirstToken = false
	m.stream.tokenCh = nil
	m.stream.errCh = nil
	m.stream.doneCh = nil
	m.startupMessages = startupMessages
	m.statusMessage = text.SwitchedToMode(mode.Name)
	m.styles = NewStyles(mode.ID)
	m.updateViewportContent()

	slog.Info("Switched mode", "mode", mode.Name, "source", "User")
}

func (m *Model) persistCurrentSession(ctx context.Context) error {
	if m.sessionStore == nil || m.currentMode.ID != agent.ModeAnalyst {
		return nil
	}

	if len(m.session.Recent) == 0 {
		return nil
	}

	runner, err := m.ensureRunner()
	if err != nil {
		return err
	}

	if err := runner.Service.FinalizeSession(ctx, &m.session); err != nil {
		return err
	}

	return m.sessionStore.SaveSession(ctx, m.currentMode.ID, m.session)
}

func (m Model) shouldWarnAboutDiscard() bool {
	return m.currentMode.ID == agent.ModeAnalyst && len(m.session.Recent) > 0
}

func (m *Model) warnBeforeDiscardingOnQuit() bool {
	if !m.shouldWarnAboutDiscard() || m.pendingQuit {
		return false
	}

	m.pendingQuit = true
	m.pendingSwitch = ""
	m.err = nil
	m.statusMessage = text.UnsavedSessionQuitMsg
	return true
}

func (m Model) handleCtrlC() (tea.Model, tea.Cmd) {
	if m.shuttingDown {
		return m, nil
	}

	if m.warnBeforeDiscardingOnQuit() {
		return m, nil
	}

	if m.pendingQuit {
		return m.beginShutdown()
	}

	m.pendingQuit = true
	m.pendingSwitch = ""
	m.err = nil
	m.statusMessage = text.ShutdownWarning
	return m, nil
}

func (m Model) beginShutdown() (tea.Model, tea.Cmd) {
	m.shuttingDown = true
	m.err = nil
	m.statusMessage = text.ShuttingDown
	m.modeSelector.active = false
	m.hintsOverlay.active = false
	m.updateViewportContent()
	return m, shutdownCmd(m.shutdownFn)
}

func shutdownCmd(shutdownFn func() error) tea.Cmd {
	return func() tea.Msg {
		if shutdownFn == nil {
			return shutdownCompleteMsg{}
		}
		return shutdownCompleteMsg{err: shutdownFn()}
	}
}

func (m *Model) clearDiscardWarnings() {
	m.pendingQuit = false
	m.pendingSwitch = ""
}

func (m Model) handleStreamError(err error) (tea.Model, tea.Cmd) {
	m.stream.active = false
	m.stream.waitingForFirstToken = false
	m.stream.cancelCurrent = nil
	m.stream.completed = false
	m.err = err
	m.clearDiscardWarnings()
	m.statusMessage = ""
	m.updateViewportContent()
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
	switch {
	case m.isModeCommandInput():
		m.hintsOverlay.active = false
		m.modeSelector.active = true
		m.resetModeSelectorIndex()
	case m.isHintsCommandInput():
		m.modeSelector.active = false
		m.modeSelector.index = 0
		m.hintsOverlay.active = true
	default:
		m.modeSelector.active = false
		m.modeSelector.index = 0
		m.hintsOverlay.active = false
	}
}

func (m Model) isModeCommandInput() bool {
	switch firstCommandToken(m.input) {
	case "/mode", "/modes":
		return true
	default:
		return false
	}
}

func (m Model) isHintsCommandInput() bool {
	return firstCommandToken(m.input) == "/hints"
}

func firstCommandToken(input string) string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

func (m *Model) refreshStartupMessages(ctx context.Context) error {
	messages, err := m.startupMessagesForSession(ctx)
	if err != nil {
		return err
	}

	m.startupMessages = messages
	m.updateViewportContent()
	return nil
}

func (m *Model) startupMessagesForSession(ctx context.Context) ([]string, error) {
	runner, err := m.ensureRunner()
	if err != nil {
		return nil, err
	}

	return runner.Service.StartupMessages(ctx, m.session)
}

func (m Model) handleStreamDone(msg agent.StreamResult) (tea.Model, tea.Cmd) {
	m.stream.active = false
	m.stream.waitingForFirstToken = false
	m.stream.cancelCurrent = nil
	m.stream.completed = !msg.Canceled
	m.session = msg.Session
	m.err = nil

	hasOutput := strings.TrimSpace(m.output) != ""
	keepStartupMessages := msg.Canceled && !hasOutput

	if msg.Canceled && !hasOutput && m.pendingPrompt != "" {
		m.input = m.pendingPrompt
	} else if hasOutput {
		m.session.AddTurn(agent.Turn{
			User:      m.pendingPrompt,
			Assistant: m.output,
		})
	}

	m.pendingPrompt = ""
	m.output = ""
	m.clearDiscardWarnings()
	if !keepStartupMessages {
		m.startupMessages = nil
	}
	m.updateViewportContent()

	return m, nil
}
