package setup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"project-orb/internal/agent"
	"project-orb/internal/llm"
	"project-orb/internal/text"
	"project-orb/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	loadingAnimationInterval = 120 * time.Millisecond
)

type setupState int

const (
	setupStateCheckInstall setupState = iota
	setupStateWaitInstallConfirm
	setupStateInstalling
	setupStateWaitUpgradeConfirm
	setupStateUpgrading
	setupStateWaitModelsDir
	setupStateWaitChatModel
	setupStateWaitEmbeddingModel
	setupStateSaving
	setupStateStartingServer
	setupStateWaitPersonaName
	setupStateWaitPersonaTone
	setupStateSavingPersona
	setupStateWaitModeSelection
	setupStateDone
	setupStateError
)

type Model struct {
	width  int
	height int
	state  setupState

	input        string
	conversation []setupMessage
	err          error

	viewport ui.ChatViewport

	isInstalled bool
	isOutdated  bool

	existingConfig  *llm.Config
	modelsDir       string
	availableModels []string
	chatModel       string
	embeddingModel  string
	selectedMode    string

	personaPath       string
	personaName       string
	personaTone       string
	needsPersonaSetup bool

	loadingFrame  int
	loadingText   string
	hintsOverlay  bool
	statusMessage string
	pendingQuit   bool
	shuttingDown  bool

	ctx    context.Context
	wizard *Wizard

	result *Result
	styles ui.Styles
}

func (m Model) Result() *Result {
	return m.result
}

type setupMessage struct {
	speaker string
	text    string
}

type setupInstallCheckDoneMsg struct {
	isInstalled bool
	isOutdated  bool
	err         error
}

type setupConfigLoadedMsg struct {
	config *llm.Config
	err    error
}

type setupModelsScanDoneMsg struct {
	models []string
	err    error
}

type setupServerStartDoneMsg struct {
	manager *llm.Manager
	err     error
}

type setupPersonaCheckDoneMsg struct {
	exists      bool
	personaPath string
	err         error
}

type setupInstallDoneMsg struct {
	err error
}

type setupUpgradeDoneMsg struct {
	err error
}

type setupConfigSaveDoneMsg struct {
	err error
}

type setupPersonaSaveDoneMsg struct {
	err error
}

type setupLoadingTickMsg struct{}
type setupShutdownCompleteMsg struct {
	err error
}

func NewModel(ctx context.Context) Model {
	m := Model{
		ctx:         ctx,
		wizard:      NewWizard(ctx),
		state:       setupStateCheckInstall,
		styles:      ui.NewStyles(agent.ModeSetup),
		loadingText: text.CheckingSystemConfiguration,
	}
	// Add greeting message first, then show loading animation
	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    text.SetupGreeting,
	})
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.checkInstallation(),
		loadingTick(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Resize(m.width, m.height, 0)
		m.updateViewportContent()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case setupInstallCheckDoneMsg:
		return m.handleInstallCheckDone(msg)

	case setupConfigLoadedMsg:
		return m.handleConfigLoaded(msg)

	case setupModelsScanDoneMsg:
		return m.handleModelsScanDone(msg)

	case setupServerStartDoneMsg:
		return m.handleServerStartDone(msg)

	case setupPersonaCheckDoneMsg:
		return m.handlePersonaCheckDone(msg)

	case setupInstallDoneMsg:
		return m.handleError(msg.err)

	case setupUpgradeDoneMsg:
		return m.handleError(msg.err)

	case setupConfigSaveDoneMsg:
		return m.handleError(msg.err)

	case setupPersonaSaveDoneMsg:
		return m.handleError(msg.err)

	case error:
		return m.handleError(msg)

	case setupLoadingTickMsg:
		if m.state == setupStateStartingServer || m.state == setupStateCheckInstall {
			textLen := len([]rune(m.loadingText))
			if textLen > 0 {
				m.loadingFrame = (m.loadingFrame + 1) % textLen
			}
			slog.Debug("Loading tick", "frame", m.loadingFrame)
			m.updateViewportContent()
			return m, loadingTick()
		}
		return m, nil
	case setupShutdownCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			m.shuttingDown = false
			m.pendingQuit = true
			m.statusMessage = text.ShutdownFailed
			return m, nil
		}
		return m, tea.Quit
	}

	return m, m.viewport.Update(msg)
}

func (m *Model) updateViewportContent() {
	if !m.viewport.Ready() {
		return
	}

	contentWidth := ui.ChatContentWidth(m.width)
	content := m.renderConversationContent(contentWidth)
	m.viewport.SetContent(content)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	inputActive := m.acceptsInput()

	switch msg.Type {
	case tea.KeyCtrlC:
		return m.handleCtrlC()

	case tea.KeyEsc:
		m.clearShutdownWarning()
		if m.hintsOverlay {
			m.hintsOverlay = false
			return m, nil
		}
		return m, nil

	case tea.KeyEnter:
		m.clearShutdownWarning()
		if !inputActive {
			return m, nil
		}
		return m.handleEnter()

	case tea.KeyBackspace, tea.KeyDelete:
		m.clearShutdownWarning()
		if !inputActive || len(m.input) == 0 {
			return m, nil
		}
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		m.syncSlashCommandUI()
		return m, nil

	case tea.KeySpace:
		m.clearShutdownWarning()
		if !inputActive {
			return m, nil
		}
		m.input += " "
		m.syncSlashCommandUI()
		return m, nil

	default:
		m.clearShutdownWarning()
		if !inputActive {
			return m, nil
		}
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.syncSlashCommandUI()
		}
		return m, nil
	}
}

func (m Model) handleCtrlC() (tea.Model, tea.Cmd) {
	if m.shuttingDown {
		return m, nil
	}

	if !m.pendingQuit {
		m.pendingQuit = true
		m.err = nil
		m.statusMessage = text.ShutdownWarning
		return m, nil
	}

	if m.state == setupStateStartingServer {
		slog.Info("Setup interrupted by user during server start")
	}

	m.shuttingDown = true
	m.statusMessage = text.ShuttingDown
	m.hintsOverlay = false
	return m, m.shutdownCmd()
}

func (m Model) shutdownCmd() tea.Cmd {
	manager := m.ensureResult().Manager
	return func() tea.Msg {
		if manager == nil {
			return setupShutdownCompleteMsg{}
		}
		return setupShutdownCompleteMsg{err: manager.Shutdown()}
	}
}

func (m *Model) clearShutdownWarning() {
	m.pendingQuit = false
	if m.statusMessage == text.ShutdownWarning {
		m.statusMessage = ""
	}
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	userInput := strings.TrimSpace(m.input)

	// Allow empty input for persona customization states (to skip)
	allowEmpty := m.state == setupStateWaitPersonaName ||
		m.state == setupStateWaitPersonaTone

	if userInput == "" && !allowEmpty {
		return m, nil
	}

	if firstSetupCommandToken(userInput) == "/hints" {
		m.hintsOverlay = true
		m.input = ""
		return m, nil
	}

	// Only add to conversation if there's actual input
	if userInput != "" {
		m.conversation = append(m.conversation, setupMessage{speaker: "user", text: userInput})
		m.updateViewportContent()
	}
	m.input = ""

	switch m.state {
	case setupStateWaitInstallConfirm:
		return m.handleInstallConfirm(userInput)

	case setupStateWaitUpgradeConfirm:
		return m.handleUpgradeConfirm(userInput)

	case setupStateWaitModelsDir:
		return m.handleModelsDir(userInput)

	case setupStateWaitChatModel:
		return m.handleChatModelSelection(userInput)

	case setupStateWaitEmbeddingModel:
		return m.handleEmbeddingModelSelection(userInput)

	case setupStateWaitModeSelection:
		return m.handleModeSelection(userInput)

	case setupStateWaitPersonaName:
		return m.handlePersonaNameInput(userInput)

	case setupStateWaitPersonaTone:
		return m.handlePersonaToneInput(userInput)
	}

	return m, nil
}

func (m Model) acceptsInput() bool {
	switch m.state {
	case setupStateWaitInstallConfirm,
		setupStateWaitUpgradeConfirm,
		setupStateWaitModelsDir,
		setupStateWaitChatModel,
		setupStateWaitEmbeddingModel,
		setupStateWaitPersonaName,
		setupStateWaitPersonaTone,
		setupStateWaitModeSelection:
		return true
	default:
		return false
	}
}

func (m *Model) syncSlashCommandUI() {
	m.hintsOverlay = firstSetupCommandToken(m.input) == "/hints"
}

func firstSetupCommandToken(input string) string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

func (m Model) checkInstallation() tea.Cmd {
	return func() tea.Msg {
		isInstalled, isOutdated, err := llm.CheckInstallation()
		return setupInstallCheckDoneMsg{
			isInstalled: isInstalled,
			isOutdated:  isOutdated,
			err:         err,
		}
	}
}

func (m Model) handleInstallCheckDone(msg setupInstallCheckDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.state = setupStateError
		return m, nil
	}

	m.isInstalled = msg.isInstalled
	m.isOutdated = msg.isOutdated

	if !m.isInstalled {
		m.addWizardMessage(text.LlamaServerNotInstalled)
		m.state = setupStateWaitInstallConfirm
		return m, nil
	}

	if m.isOutdated {
		m.addWizardMessage(text.LlamaServerOutdated)
		m.state = setupStateWaitUpgradeConfirm
		return m, nil
	}

	// Installation is good, check config
	return m, m.loadConfig()
}

func (m Model) startLoadingState(config *llm.Config) (tea.Model, tea.Cmd) {
	m.state = setupStateStartingServer
	m.loadingFrame = 0
	m.loadingText = ui.LoadingModelText
	return m, tea.Batch(
		m.startServer(config),
		loadingTick(),
	)
}

func (m Model) continueWithSavedConfig(startMessage string) (tea.Model, tea.Cmd) {
	if startMessage != "" {
		m.addWizardMessage(startMessage)
	}

	slog.Info("Starting server")

	config, err := llm.LoadConfig()
	if err != nil {
		m.addWizardMessage(text.FailedToLoadConfig(err))
		m.state = setupStateError
		return m, nil
	}

	return m.startLoadingState(config)
}

func (m Model) handleInstallConfirm(input string) (tea.Model, tea.Cmd) {
	if !IsYesOrNo(input) {
		m.addWizardMessage(text.InstallLlamaServerPrompt())
		return m, nil
	}

	if !IsYes(input) {
		m.addWizardMessage(text.LlamaServerRequired)
		m.state = setupStateError
		return m, nil
	}

	m.addWizardMessage(text.InstallingLlamaCpp)
	m.state = setupStateInstalling
	return m, m.installLlamaCpp()
}

func (m Model) installLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		return setupInstallDoneMsg{err: llm.InstallViaBrew()}
	}
}

func (m Model) handleError(err error) (tea.Model, tea.Cmd) {
	if err == nil {
		switch m.state {
		case setupStateInstalling:
			m.addWizardMessage(text.InstallationComplete)
			return m, m.loadConfig()

		case setupStateUpgrading:
			m.addWizardMessage(text.UpdateComplete)
			return m, m.loadConfig()

		case setupStateSaving:
			m.addWizardMessage(text.ConfigurationSaved)

			if m.needsPersonaSetup {
				m.needsPersonaSetup = false
				m.addWizardMessage(text.PersonalizeAgent)
				m.state = setupStateWaitPersonaName
				return m, nil
			}

			return m.continueWithSavedConfig(text.StartingServer)

		case setupStateSavingPersona:
			m.addWizardMessage(text.PersonaSaved(m.personaPath))
			return m.continueWithSavedConfig(text.StartingServer)
		}
		return m, nil
	}

	// Handle error based on current state
	switch m.state {
	case setupStateInstalling:
		m.addWizardMessage(text.InstallationFailed(err))
		m.state = setupStateError
		return m, nil

	case setupStateUpgrading:
		m.addWizardMessage(text.UpdateFailed(err))
		return m, m.loadConfig()

	case setupStateSaving:
		m.addWizardMessage(text.FailedToSaveConfig(err))
		m.state = setupStateError
		return m, nil

	case setupStateSavingPersona:
		return m.continueWithSavedConfig(text.FailedToSavePersona(err, m.personaPath))

	default:
		m.err = err
		m.state = setupStateError
		return m, nil
	}
}

func (m Model) handleUpgradeConfirm(input string) (tea.Model, tea.Cmd) {
	if !IsYesOrNo(input) {
		m.addWizardMessage(text.UpgradeLlamaCppPrompt)
		return m, nil
	}

	if !IsYes(input) {
		m.addWizardMessage(text.ContinueWithCurrentVersion)
		return m, m.loadConfig()
	}

	m.addWizardMessage(text.UpdatingLlamaCpp)
	m.state = setupStateUpgrading
	return m, m.upgradeLlamaCpp()
}

func (m Model) upgradeLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		return setupUpgradeDoneMsg{err: llm.UpgradeViaBrew()}
	}
}

func (m Model) loadConfig() tea.Cmd {
	return func() tea.Msg {
		config, err := llm.LoadConfig()
		return setupConfigLoadedMsg{
			config: config,
			err:    err,
		}
	}
}

func (m Model) handleConfigLoaded(msg setupConfigLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil && msg.config != nil {
		m.existingConfig = msg.config
		m.addWizardMessage(text.FoundConfiguration(
			msg.config.LlamaCpp.ModelsDir,
			msg.config.LlamaCpp.ChatModel,
			msg.config.LlamaCpp.EmbeddingModel,
		))
		return m.startLoadingState(m.existingConfig)
	}

	m.addWizardMessage(text.AskForModelsDir(m.wizard.DefaultModelsDirInput()))
	m.input = m.wizard.DefaultModelsDirInput()
	m.state = setupStateWaitModelsDir
	return m, nil
}

func (m Model) handleModelsDir(input string) (tea.Model, tea.Cmd) {
	absPath, err := m.wizard.ValidateModelsDir(input)
	if err != nil {
		m.input = input
		m.addWizardMessage(fmt.Sprintf("%v\n%s", err, text.TryAgainPrompt))
		return m, nil
	}

	m.modelsDir = absPath
	return m, m.scanModels()
}

func (m Model) scanModels() tea.Cmd {
	return func() tea.Msg {
		models, err := m.wizard.ScanModels(m.modelsDir)
		return setupModelsScanDoneMsg{models: models, err: err}
	}
}

func (m Model) handleModelsScanDone(msg setupModelsScanDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(text.FailedToScanDirectory(msg.err))
		m.state = setupStateWaitModelsDir
		return m, nil
	}

	if len(msg.models) == 0 {
		m.addWizardMessage(text.NoGgufFilesFound)
		m.state = setupStateWaitModelsDir
		return m, nil
	}

	m.availableModels = msg.models
	m.addWizardMessage(text.FoundModels(msg.models))
	m.state = setupStateWaitChatModel
	return m, nil
}

func (m Model) handleChatModelSelection(input string) (tea.Model, tea.Cmd) {
	selection, err := ValidateModelSelection(input, len(m.availableModels))
	if err != nil {
		m.addWizardMessage(err.Error())
		return m, nil
	}

	m.chatModel = m.availableModels[selection-1]
	m.addWizardMessage(text.AskForEmbeddingModel(m.chatModel, len(m.availableModels)))
	m.state = setupStateWaitEmbeddingModel
	return m, nil
}

func (m Model) handleEmbeddingModelSelection(input string) (tea.Model, tea.Cmd) {
	selection, err := ValidateModelSelection(input, len(m.availableModels))
	if err != nil {
		m.addWizardMessage(err.Error())
		return m, nil
	}

	m.embeddingModel = m.availableModels[selection-1]
	m.addWizardMessage(text.SelectedEmbeddingModel(m.embeddingModel))

	return m, m.checkPersona()
}

func (m Model) saveConfig() tea.Cmd {
	return func() tea.Msg {
		config := &llm.Config{
			LlamaCpp: llm.LlamaCppConfig{
				ModelsDir:      m.modelsDir,
				ChatModel:      m.chatModel,
				EmbeddingModel: m.embeddingModel,
				ChatPort:       8080,
				EmbeddingPort:  8081,
			},
		}

		return setupConfigSaveDoneMsg{err: llm.SaveConfig(config)}
	}
}

func (m Model) checkPersona() tea.Cmd {
	return func() tea.Msg {
		exists, personaPath, err := m.wizard.CheckPersonaExists()
		return setupPersonaCheckDoneMsg{
			exists:      exists,
			personaPath: personaPath,
			err:         err,
		}
	}
}

func (m Model) handlePersonaCheckDone(msg setupPersonaCheckDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(text.FailedToSetupPersona(msg.err))
		m.needsPersonaSetup = false
		m.state = setupStateSaving
		return m, m.saveConfig()
	}

	m.personaPath = msg.personaPath
	m.needsPersonaSetup = !msg.exists

	m.state = setupStateSaving
	return m, m.saveConfig()
}

func (m Model) startServer(config *llm.Config) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Starting chat server")

		// Create manager first so we can clean it up if interrupted
		manager, err := m.wizard.StartServer(config)

		slog.Info("Server start completed", "error", err)
		return setupServerStartDoneMsg{manager: manager, err: err}
	}
}

func (m Model) handleServerStartDone(msg setupServerStartDoneMsg) (tea.Model, tea.Cmd) {
	// Store manager reference even if there's an error, so it can be cleaned up
	if msg.manager != nil {
		m.ensureResult().Manager = msg.manager
	}

	if msg.err != nil {
		m.addWizardMessage(text.FailedToStartServer(msg.err))
		m.state = setupStateError
		return m, nil
	}

	m.state = setupStateWaitModeSelection
	m.addWizardMessage(text.ServerReady)
	return m, nil
}

func (m *Model) addWizardMessage(text string) {
	m.conversation = append(m.conversation, setupMessage{speaker: "wizard", text: text})
	m.updateViewportContent()
}

func (m *Model) ensureResult() *Result {
	if m.result == nil {
		m.result = &Result{}
	}
	return m.result
}

func (m Model) handleModeSelection(input string) (tea.Model, tea.Cmd) {
	selection, err := ValidateModeSelection(input)
	if err != nil {
		m.addWizardMessage(text.InvalidModeSelection)
		return m, nil
	}

	m.selectedMode = ModeIDFromSelection(selection)
	m.ensureResult().SelectedMode = m.selectedMode
	m.state = setupStateDone

	return m, tea.Quit
}

func (m Model) handlePersonaNameInput(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)
	if input != "" {
		m.personaName = input
		m.addWizardMessage(text.AgentNamed(input))
		m.addWizardMessage(text.AskForPersonality(input))
	} else {
		m.addWizardMessage(text.AskForPersonality(text.DefaultAgentSubject))
	}
	m.state = setupStateWaitPersonaTone
	return m, nil
}

func (m Model) handlePersonaToneInput(input string) (tea.Model, tea.Cmd) {
	m.personaTone = strings.TrimSpace(input)
	// Empty is fine - BuildPersona will use default.
	m.state = setupStateSavingPersona
	return m, m.savePersona()
}

func (m Model) savePersona() tea.Cmd {
	return func() tea.Msg {
		content := m.wizard.BuildPersona(m.personaName, m.personaTone)
		return setupPersonaSaveDoneMsg{err: m.wizard.SavePersona(m.personaPath, content)}
	}
}

func loadingTick() tea.Cmd {
	return tea.Tick(loadingAnimationInterval, func(time.Time) tea.Msg {
		return setupLoadingTickMsg{}
	})
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return text.LoadingPlaceholder
	}

	contentWidth := ui.ChatContentWidth(m.width)
	chatHeight := ui.ChatPaneHeight(m.height, 0)
	content := m.renderConversationContent(contentWidth)
	if m.viewport.Ready() {
		content = m.viewport.View()
	}

	var inputPane string
	if m.state == setupStateDone || m.state == setupStateError {
		inputText := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSubdued)).Italic(true).Render(text.PressCtrlCToExit)
		inputPane = ui.RenderInputMessageBox(m.width, m.styles.InputBox, inputText)
	} else {
		inputPane = ui.RenderInputBox(m.width, m.styles.InputBox, ui.ThemeForMode(agent.ModeSetup).Border, m.input, m.acceptsInput(), text.TypeYourResponsePrompt)
	}

	var extraPane string
	if m.hintsOverlay {
		extraPane = m.renderHintsOverlay(m.width, 10)
	}

	modeName := m.styles.StatusBarStyle.Render(text.SetupModeLabel)
	hints := ui.NeutralHelpStyle.Render(text.SetupStatusHints)
	statusBar := ui.RenderStatusBar(m.width, m.styles.StatusBarStyle, modeName, "", hints)

	return ui.RenderChatShell(m.width, chatHeight, content, m.statusMessage, inputPane, extraPane, statusBar)
}

func (m Model) renderConversationContent(contentWidth int) string {
	loadingText := m.renderLoadingMessage()

	var blocks []string
	for i, msg := range m.conversation {
		body := msg.text
		if loadingText != "" && msg.speaker == "wizard" && i == len(m.conversation)-1 {
			body = strings.TrimRight(body, "\n")
			if strings.TrimSpace(body) != "" {
				body += "\n\n" + loadingText
			} else {
				body = loadingText
			}
			loadingText = ""
		}

		if msg.speaker == "wizard" {
			blocks = append(blocks, ui.RenderAgentBlock(contentWidth, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, text.WizardLabel, body))
		} else {
			blocks = append(blocks, ui.RenderUserBlock(contentWidth, m.styles.UserNameStyle, m.styles.UserBodyStyle, text.UserLabel, body))
		}
	}

	if loadingText != "" {
		blocks = append(blocks, ui.RenderAgentBlock(contentWidth, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, text.WizardLabel, loadingText))
	}

	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m Model) renderHintsOverlay(width int, maxLines int) string {
	if !m.hintsOverlay || maxLines <= 2 {
		return ""
	}

	lines := []string{
		ui.NeutralSelectorTitleStyle.Render(text.HintsTitle) + "  " + ui.NeutralHelpStyle.Render(text.HintsCloseHint),
		text.SetupHintsShowHelp,
		text.SetupHintsSend,
		"⇧+drag          select and copy text in supported terminals",
		text.HintsScrollConversation,
		text.SetupHintsQuit,
	}

	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")
	return m.styles.SelectorBoxStyle.Width(width).Render(content)
}

func (m Model) renderLoadingMessage() string {
	if m.state != setupStateStartingServer && m.state != setupStateCheckInstall {
		return ""
	}

	return ui.RenderLoadingAnimation(m.loadingFrame, m.loadingText, ui.ThinkingColorBright, ui.ThinkingColorMedium, ui.ThinkingColorDim, ui.ThinkingColorSubdued)
}

type Result struct {
	Manager      *llm.Manager
	SelectedMode string
}
