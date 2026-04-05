package setup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"project-orb/internal/llm"
	"project-orb/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	loadingAnimationInterval = 120 * time.Millisecond
	loadingDotsFrames        = 4
	loadingDotsDivisor       = 3

	wizardGreeting = "Hello! I'm your setup wizard. I'll help you get everything configured to start using the application.\n\n"
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

	isInstalled bool
	isOutdated  bool

	existingConfig  *llm.Config
	modelsDir       string
	availableModels []string
	chatModel       string
	embeddingModel  string
	selectedMode    string

	personaPath string
	personaName string
	personaTone string

	loadingFrame int

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

type setupLoadingTickMsg struct{}

func NewModel(ctx context.Context) Model {
	return Model{
		ctx:    ctx,
		wizard: NewWizard(ctx),
		state:  setupStateCheckInstall,
		styles: ui.NewStyles(ui.ModeSetup),
	}
}

func (m Model) Init() tea.Cmd {
	return m.checkInstallation()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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

	case error:
		return m.handleError(msg)

	case setupLoadingTickMsg:
		if m.state == setupStateStartingServer {
			m.loadingFrame = (m.loadingFrame + 1) % 12
			slog.Debug("Loading tick", "frame", m.loadingFrame)
			return m, loadingTick()
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		// If we're in the middle of starting a server, mark as interrupted
		if m.state == setupStateStartingServer {
			slog.Info("Setup interrupted by user during server start")
		}
		return m, tea.Quit

	case tea.KeyEnter:
		return m.handleEnter()

	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.input) == 0 {
			return m, nil
		}
		runes := []rune(m.input)
		m.input = string(runes[:len(runes)-1])
		return m, nil

	case tea.KeySpace:
		m.input += " "
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		}
		return m, nil
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

	// Only add to conversation if there's actual input
	if userInput != "" {
		m.conversation = append(m.conversation, setupMessage{speaker: "user", text: userInput})
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
		m.conversation = append(m.conversation, setupMessage{
			speaker: "wizard",
			text:    wizardGreeting + "I noticed llama-server isn't installed on your system. I can install it via Homebrew for you. Would you like me to install it? (yes/no)",
		})
		m.state = setupStateWaitInstallConfirm
		return m, nil
	}

	if m.isOutdated {
		m.conversation = append(m.conversation, setupMessage{
			speaker: "wizard",
			text:    wizardGreeting + "I noticed there's a newer version of llama.cpp available via Homebrew. Would you like to update it? (yes/no)",
		})
		m.state = setupStateWaitUpgradeConfirm
		return m, nil
	}

	// Installation is good, check config
	return m, m.loadConfig()
}

func (m Model) handleInstallConfirm(input string) (tea.Model, tea.Cmd) {
	if !IsYesOrNo(input) {
		m.addWizardMessage("Please answer yes or no. Would you like me to install llama-server? (yes/no)")
		return m, nil
	}

	if !IsYes(input) {
		m.addWizardMessage("I understand. Unfortunately, llama-server is required to run this application. Please install it manually and try again.")
		m.state = setupStateError
		return m, nil
	}

	m.addWizardMessage("Great! Installing llama.cpp via Homebrew... This may take a few minutes.")
	m.state = setupStateInstalling
	return m, m.installLlamaCpp()
}

func (m Model) installLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		return llm.InstallViaBrew()
	}
}

func (m Model) handleError(err error) (tea.Model, tea.Cmd) {
	if err == nil {
		switch m.state {
		case setupStateInstalling:
			m.addWizardMessage("Installation complete!")
			return m, m.loadConfig()

		case setupStateUpgrading:
			m.addWizardMessage("Update complete!")
			return m, m.loadConfig()

		case setupStateSaving:
			m.conversation = append(m.conversation, setupMessage{
				speaker: "wizard",
				text:    "Configuration saved! \nStarting the chat server... This may take a moment while the model loads.",
			})
			m.state = setupStateStartingServer
			m.loadingFrame = 0

			slog.Info("Starting server")

			config, err := llm.LoadConfig()
			if err != nil {
				m.addWizardMessage(fmt.Sprintf("Failed to load saved configuration: %v", err))
				m.state = setupStateError
				return m, nil
			}

			return m, tea.Batch(
				m.startServer(config),
				loadingTick(),
			)

		case setupStateSavingPersona:
			m.addWizardMessage("Persona saved! \nSaving configuration...")
			m.state = setupStateSaving
			return m, m.saveConfig()
		}
		return m, nil
	}

	// Handle error based on current state
	switch m.state {
	case setupStateInstalling:
		m.addWizardMessage(fmt.Sprintf("Installation failed: %v\nPlease install llama.cpp manually and try again.", err))
		m.state = setupStateError
		return m, nil

	case setupStateUpgrading:
		m.addWizardMessage(fmt.Sprintf("Update failed: %v\nContinuing with current version.", err))
		return m, m.loadConfig()

	case setupStateSaving:
		m.addWizardMessage(fmt.Sprintf("Failed to save configuration: %v", err))
		m.state = setupStateError
		return m, nil

	case setupStateSavingPersona:
		m.addWizardMessage(fmt.Sprintf("Failed to save persona: %v\nYou can manually edit it later at:\n%s\nSaving configuration...", err, m.personaPath))
		m.state = setupStateSaving
		return m, m.saveConfig()

	default:
		m.err = err
		m.state = setupStateError
		return m, nil
	}
}

func (m Model) handleUpgradeConfirm(input string) (tea.Model, tea.Cmd) {
	if !IsYesOrNo(input) {
		m.addWizardMessage("Please answer yes or no.\nWould you like to update llama.cpp? (yes/no)")
		return m, nil
	}

	if !IsYes(input) {
		m.addWizardMessage("No problem, we'll continue with the current version.")
		return m, m.loadConfig()
	}

	m.addWizardMessage("Updating llama.cpp via Homebrew...")
	m.state = setupStateUpgrading
	return m, m.upgradeLlamaCpp()
}

func (m Model) upgradeLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		return llm.UpgradeViaBrew()
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
		m.conversation = append(m.conversation, setupMessage{
			speaker: "wizard",
			text: fmt.Sprintf(
				"Hello! I'm your setup wizard.\n"+
					"Found your configuration:\n"+
					"  Models: %s\n"+
					"  Chat: %s\n"+
					"  Embedding: %s\n\n"+
					"Starting the chat server... This may take a moment while the model loads.",
				msg.config.LlamaCpp.ModelsDir,
				msg.config.LlamaCpp.ChatModel,
				msg.config.LlamaCpp.EmbeddingModel,
			),
		})
		m.state = setupStateStartingServer
		m.loadingFrame = 0
		return m, tea.Batch(
			m.startServer(m.existingConfig),
			loadingTick(),
		)
	}

	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    wizardGreeting + "Now let's configure your models.\nWhere do you keep your .gguf model files?\n(Enter the full path, e.g., /Users/username/models)",
	})
	m.state = setupStateWaitModelsDir
	return m, nil
}

func (m Model) handleModelsDir(input string) (tea.Model, tea.Cmd) {
	absPath, err := m.wizard.ValidateModelsDir(input)
	if err != nil {
		m.addWizardMessage(fmt.Sprintf("%v\nPlease try again:", err))
		return m, nil
	}

	m.modelsDir = absPath
	m.addWizardMessage("Scanning for .gguf files...")
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
		m.addWizardMessage(fmt.Sprintf("Failed to scan directory: %v\nPlease try a different path:", msg.err))
		m.state = setupStateWaitModelsDir
		return m, nil
	}

	if len(msg.models) == 0 {
		m.addWizardMessage("No .gguf files found in that directory. Please add model files or try a different path:")
		m.state = setupStateWaitModelsDir
		return m, nil
	}

	m.availableModels = msg.models

	var modelList strings.Builder
	modelList.WriteString("I found these models:\n")
	for i, model := range msg.models {
		modelList.WriteString(fmt.Sprintf("  %d. %s\n", i+1, model))
	}
	modelList.WriteString(fmt.Sprintf("\nWhich one should I use for chat? (1-%d)", len(msg.models)))

	m.addWizardMessage(modelList.String())
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
	m.addWizardMessage(fmt.Sprintf("Great! Using %s for chat.\nWhich model should I use for embeddings? (1-%d)", m.chatModel, len(m.availableModels)))
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
	m.addWizardMessage(fmt.Sprintf("Perfect! Using %s for embeddings.", m.embeddingModel))

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

		return llm.SaveConfig(config)
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
		m.addWizardMessage(fmt.Sprintf("Failed to setup persona: %v\nSaving configuration...", msg.err))
		m.state = setupStateSaving
		return m, m.saveConfig()
	}

	m.personaPath = msg.personaPath

	// If persona already exists, skip customization
	if msg.exists {
		m.addWizardMessage("Persona already configured. Saving configuration...")
		m.state = setupStateSaving
		return m, m.saveConfig()
	}

	// First time setup - ask for persona customization
	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    "Let's personalize your agent!\n\nWhat would you like to name your agent? (press Enter to skip)",
	})
	m.state = setupStateWaitPersonaName
	return m, nil
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
		m.addWizardMessage(fmt.Sprintf("Failed to start chat server: %v", msg.err))
		m.state = setupStateError
		return m, nil
	}

	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    "Server ready! \nWhich mode would you like to start with?\n1. Coach - Guidance for everyday reflection, decisions, and next steps\n2. Performance Review - Structured feedback on effectiveness, habits and growth areas\n3. Analyst - Deeper psychoanalytic questioning to examine motives and patterns\n\nEnter 1, 2, or 3:",
	})
	m.state = setupStateWaitModeSelection
	return m, nil
}

func (m *Model) addWizardMessage(text string) {
	m.conversation = append(m.conversation, setupMessage{speaker: "wizard", text: text})
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
		m.addWizardMessage("Invalid selection. Please enter 1, 2, or 3:")
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
		m.addWizardMessage(fmt.Sprintf("Nice! Your agent will be called %s.\n\nWhat personality should %s have?\n(default: calm, thoughtful, and supportive)\n(press Enter to use default)", input, input))
	} else {
		m.addWizardMessage("What personality should the agent have?\n(default: calm, thoughtful, and supportive)\n(press Enter to use default)")
	}
	m.state = setupStateWaitPersonaTone
	return m, nil
}

func (m Model) handlePersonaToneInput(input string) (tea.Model, tea.Cmd) {
	m.personaTone = strings.TrimSpace(input)
	// Empty is fine - BuildPersona will use default

	m.addWizardMessage("Perfect! Saving your agent configuration...")
	m.state = setupStateSavingPersona
	return m, m.savePersona()
}

func (m Model) savePersona() tea.Cmd {
	return func() tea.Msg {
		content := m.wizard.BuildPersona(m.personaName, m.personaTone)
		return m.wizard.SavePersona(m.personaPath, content)
	}
}

func loadingTick() tea.Cmd {
	return tea.Tick(loadingAnimationInterval, func(time.Time) tea.Msg {
		return setupLoadingTickMsg{}
	})
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	contentWidth := m.width - (ui.ChatPadding * 2)
	chatHeight := m.height - ui.InputHeight - ui.StatusBarHeight

	var blocks []string
	for _, msg := range m.conversation {
		if msg.speaker == "wizard" {
			blocks = append(blocks, ui.RenderAgentBlock(contentWidth, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, "Setup Wizard", msg.text))
		} else {
			blocks = append(blocks, ui.RenderUserBlock(contentWidth, m.styles.UserNameStyle, m.styles.UserBodyStyle, "You", msg.text))
		}
	}

	if m.state == setupStateStartingServer {
		loadingText := ui.RenderLoadingAnimation(m.loadingFrame, "Loading model", ui.ThinkingColorBright, ui.ThinkingColorMedium, ui.ThinkingColorDim, ui.ThinkingColorSubdued)
		blocks = append(blocks, ui.RenderAgentBlock(contentWidth, m.styles.CoachNameStyle, m.styles.CoachBodyStyle, "Setup Wizard", loadingText))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	lines := strings.Split(content, "\n")
	if len(lines) > chatHeight {
		lines = lines[len(lines)-chatHeight:]
		content = strings.Join(lines, "\n")
	}

	chatPane := lipgloss.NewStyle().
		Width(m.width).
		Height(chatHeight).
		Padding(0, 1).
		Render(content)

	var inputText string
	if m.state == setupStateDone || m.state == setupStateError {
		inputText = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSubdued)).Italic(true).Render("Press Ctrl+C to exit")
	} else if m.input == "" {
		inputText = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSubdued)).Italic(true).Render(ui.InputCursor + " Type your response and press Enter")
	} else {
		inputText = m.input + ui.InputCursor
	}

	prompt := lipgloss.NewStyle().Foreground(ui.ThemeForMode(ui.ModeSetup).Border).Bold(true).Render("▸ ")
	inputPane := m.styles.InputBox.Width(m.width).Render(prompt + inputText)

	modeName := m.styles.StatusBarStyle.Render("Setup Mode")
	hints := ui.NeutralHelpStyle.Render("⏎ send · ^C quit")
	statusBar := m.styles.StatusBarStyle.Width(m.width).Render(modeName + " | " + hints)

	return lipgloss.JoinVertical(lipgloss.Left, chatPane, inputPane, statusBar)
}

type Result struct {
	Manager      *llm.Manager
	SelectedMode string
}
