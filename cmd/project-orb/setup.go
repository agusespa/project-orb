package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"project-orb/internal/agent"
	"project-orb/internal/llm"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	loadingAnimationInterval = 120 * time.Millisecond
	loadingDotsFrames        = 4
	loadingDotsDivisor       = 3
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
	setupStateWaitPersonaCustomize
	setupStateWaitPersonaName
	setupStateWaitPersonaTone
	setupStateWaitPersonaGoals
	setupStateSavingPersona
	setupStateWaitModeSelection
	setupStateDone
	setupStateError
)

type setupModel struct {
	width  int
	height int
	state  setupState

	input        string
	conversation []setupMessage
	err          error

	// Installation state
	isInstalled bool
	isOutdated  bool

	// Config state
	existingConfig  *llm.Config
	modelsDir       string
	availableModels []string
	chatModel       string
	embeddingModel  string
	selectedMode    string

	// Persona customization state
	personaPath  string
	personaName  string
	personaTone  string
	personaGoals string

	// Loading animation
	loadingFrame int

	// Context
	ctx context.Context

	// Result
	result *SetupResult
}

type setupMessage struct {
	speaker string // "wizard" or "user"
	text    string
}

type setupInstallCheckDoneMsg struct {
	isInstalled bool
	isOutdated  bool
	err         error
}

type setupInstallDoneMsg struct {
	err error
}

type setupUpgradeDoneMsg struct {
	err error
}

type setupConfigLoadedMsg struct {
	config *llm.Config
	err    error
}

type setupModelsScanDoneMsg struct {
	models []string
	err    error
}

type setupSaveConfigDoneMsg struct {
	err error
}

type setupServerStartDoneMsg struct {
	manager *llm.Manager
	err     error
}

type setupPersonaCheckDoneMsg struct {
	isDefault   bool
	personaPath string
}

type setupPersonaSaveDoneMsg struct {
	err error
}

type setupLoadingTickMsg struct{}

func newSetupModel(ctx context.Context) setupModel {
	return setupModel{
		ctx:   ctx,
		state: setupStateCheckInstall,
	}
}

func (m setupModel) Init() tea.Cmd {
	return m.checkInstallation()
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case setupInstallCheckDoneMsg:
		return m.handleInstallCheckDone(msg)

	case setupInstallDoneMsg:
		return m.handleInstallDone(msg)

	case setupUpgradeDoneMsg:
		return m.handleUpgradeDone(msg)

	case setupConfigLoadedMsg:
		return m.handleConfigLoaded(msg)

	case setupModelsScanDoneMsg:
		return m.handleModelsScanDone(msg)

	case setupSaveConfigDoneMsg:
		return m.handleSaveConfigDone(msg)

	case setupServerStartDoneMsg:
		return m.handleServerStartDone(msg)

	case setupPersonaCheckDoneMsg:
		return m.handlePersonaCheckDone(msg)

	case setupPersonaSaveDoneMsg:
		return m.handlePersonaSaveDone(msg)

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

func (m setupModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
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

func (m setupModel) handleEnter() (tea.Model, tea.Cmd) {
	userInput := strings.TrimSpace(m.input)

	// Allow empty input for persona customization states (to skip)
	allowEmpty := m.state == setupStateWaitPersonaName ||
		m.state == setupStateWaitPersonaTone ||
		m.state == setupStateWaitPersonaGoals

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

	case setupStateWaitPersonaCustomize:
		return m.handlePersonaCustomizeConfirm(userInput)

	case setupStateWaitPersonaName:
		return m.handlePersonaNameInput(userInput)

	case setupStateWaitPersonaTone:
		return m.handlePersonaToneInput(userInput)

	case setupStateWaitPersonaGoals:
		return m.handlePersonaGoalsInput(userInput)
	}

	return m, nil
}

func (m setupModel) checkInstallation() tea.Cmd {
	return func() tea.Msg {
		isInstalled, isOutdated, err := llm.CheckInstallation()
		return setupInstallCheckDoneMsg{
			isInstalled: isInstalled,
			isOutdated:  isOutdated,
			err:         err,
		}
	}
}

func (m setupModel) handleInstallCheckDone(msg setupInstallCheckDoneMsg) (tea.Model, tea.Cmd) {
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
			text:    "Hello! I'm your setup wizard.\n\nI'll help you get everything configured to start using the application.\n\nI noticed llama-server isn't installed on your system. I can install it via Homebrew for you.\n\nWould you like me to install it? (yes/no)",
		})
		m.state = setupStateWaitInstallConfirm
		return m, nil
	}

	if m.isOutdated {
		m.conversation = append(m.conversation, setupMessage{
			speaker: "wizard",
			text:    "Hello! I'm your setup wizard.\n\nI'll help you get everything configured to start using the application.\n\nI noticed there's a newer version of llama.cpp available via Homebrew.\n\nWould you like to update it? (yes/no)",
		})
		m.state = setupStateWaitUpgradeConfirm
		return m, nil
	}

	// Installation is good, check config
	return m, m.loadConfig()
}

func (m setupModel) handleInstallConfirm(input string) (tea.Model, tea.Cmd) {
	if !isYesOrNo(input) {
		m.addWizardMessage("Please answer yes or no.\n\nWould you like me to install llama-server? (yes/no)")
		return m, nil
	}

	if !isYes(input) {
		m.addWizardMessage("I understand. Unfortunately, llama-server is required to run this application. Please install it manually and try again.")
		m.state = setupStateError
		return m, nil
	}

	m.addWizardMessage("Great! Installing llama.cpp via Homebrew... This may take a few minutes.")
	m.state = setupStateInstalling
	return m, m.installLlamaCpp()
}

func (m setupModel) installLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		err := llm.InstallViaBrew()
		return setupInstallDoneMsg{err: err}
	}
}

func (m setupModel) handleInstallDone(msg setupInstallDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(fmt.Sprintf("Installation failed: %v\n\nPlease install llama.cpp manually and try again.", msg.err))
		m.state = setupStateError
		return m, nil
	}

	m.addWizardMessage("Installation complete! ✓")
	return m, m.loadConfig()
}

func (m setupModel) handleUpgradeConfirm(input string) (tea.Model, tea.Cmd) {
	if !isYesOrNo(input) {
		m.addWizardMessage("Please answer yes or no.\n\nWould you like to update llama.cpp? (yes/no)")
		return m, nil
	}

	if !isYes(input) {
		m.addWizardMessage("No problem, we'll continue with the current version.")
		return m, m.loadConfig()
	}

	m.addWizardMessage("Updating llama.cpp via Homebrew...")
	m.state = setupStateUpgrading
	return m, m.upgradeLlamaCpp()
}

func (m setupModel) upgradeLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		err := llm.UpgradeViaBrew()
		return setupUpgradeDoneMsg{err: err}
	}
}

func (m setupModel) handleUpgradeDone(msg setupUpgradeDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(fmt.Sprintf("Update failed: %v\n\nContinuing with current version.", msg.err))
	} else {
		m.addWizardMessage("Update complete! ✓")
	}
	return m, m.loadConfig()
}

func (m setupModel) loadConfig() tea.Cmd {
	return func() tea.Msg {
		config, err := llm.LoadConfig()
		return setupConfigLoadedMsg{
			config: config,
			err:    err,
		}
	}
}

func (m setupModel) handleConfigLoaded(msg setupConfigLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil && msg.config != nil {
		// Config exists and is valid - start server immediately
		m.existingConfig = msg.config
		m.conversation = append(m.conversation, setupMessage{
			speaker: "wizard",
			text: fmt.Sprintf(
				"Hello! I'm your setup wizard.\n\n"+
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

	// No config or invalid, start setup wizard
	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    "Hello! I'm your setup wizard.\n\nI'll help you get everything configured to start using the application.\n\nNow let's configure your models.\n\nWhere do you keep your .gguf model files?\n(Enter the full path, e.g., /Users/username/models)",
	})
	m.state = setupStateWaitModelsDir
	return m, nil
}

func (m setupModel) handleModelsDir(input string) (tea.Model, tea.Cmd) {
	// Expand ~ to home directory
	if strings.HasPrefix(input, "~") {
		homeDir, _ := os.UserHomeDir()
		input = filepath.Join(homeDir, input[1:])
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(input)
	if err != nil {
		m.addWizardMessage(fmt.Sprintf("Invalid path: %v\n\nPlease try again:", err))
		return m, nil
	}

	// Check if directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		m.addWizardMessage(fmt.Sprintf("Directory doesn't exist: %s\n\nPlease enter a valid directory path:", absPath))
		return m, nil
	}

	m.modelsDir = absPath
	m.addWizardMessage("Scanning for .gguf files...")
	return m, m.scanModels()
}

func (m setupModel) scanModels() tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(m.modelsDir)
		if err != nil {
			return setupModelsScanDoneMsg{err: err}
		}

		var models []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasSuffix(strings.ToLower(name), ".gguf") {
				models = append(models, name)
			}
		}

		return setupModelsScanDoneMsg{models: models, err: nil}
	}
}

func (m setupModel) handleModelsScanDone(msg setupModelsScanDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(fmt.Sprintf("Failed to scan directory: %v\n\nPlease try a different path:", msg.err))
		m.state = setupStateWaitModelsDir
		return m, nil
	}

	if len(msg.models) == 0 {
		m.addWizardMessage("No .gguf files found in that directory.\n\nPlease add model files or try a different path:")
		m.state = setupStateWaitModelsDir
		return m, nil
	}

	m.availableModels = msg.models

	var modelList strings.Builder
	modelList.WriteString("I found these models:\n\n")
	for i, model := range msg.models {
		modelList.WriteString(fmt.Sprintf("  %d. %s\n", i+1, model))
	}
	modelList.WriteString(fmt.Sprintf("\nWhich one should I use for chat? (1-%d)", len(msg.models)))

	m.addWizardMessage(modelList.String())
	m.state = setupStateWaitChatModel
	return m, nil
}

func (m setupModel) handleChatModelSelection(input string) (tea.Model, tea.Cmd) {
	if len(m.availableModels) == 0 {
		m.addWizardMessage("No models available. Please go back and select a valid models directory.")
		m.state = setupStateError
		return m, nil
	}

	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(m.availableModels) {
		m.addWizardMessage(fmt.Sprintf("Invalid selection. Please enter a number between 1 and %d:", len(m.availableModels)))
		return m, nil
	}

	m.chatModel = m.availableModels[selection-1]
	m.addWizardMessage(fmt.Sprintf("Great! Using %s for chat.\n\nWhich model should I use for embeddings? (1-%d)", m.chatModel, len(m.availableModels)))
	m.state = setupStateWaitEmbeddingModel
	return m, nil
}

func (m setupModel) handleEmbeddingModelSelection(input string) (tea.Model, tea.Cmd) {
	if len(m.availableModels) == 0 {
		m.addWizardMessage("No models available. Please go back and select a valid models directory.")
		m.state = setupStateError
		return m, nil
	}

	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(m.availableModels) {
		m.addWizardMessage(fmt.Sprintf("Invalid selection. Please enter a number between 1 and %d:", len(m.availableModels)))
		return m, nil
	}

	m.embeddingModel = m.availableModels[selection-1]
	m.addWizardMessage(fmt.Sprintf("Perfect! Using %s for embeddings.", m.embeddingModel))

	// Check persona before saving config
	return m, m.checkPersona()
}

func (m setupModel) saveConfig() tea.Cmd {
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

		err := llm.SaveConfig(config)
		return setupSaveConfigDoneMsg{err: err}
	}
}

func (m setupModel) handleSaveConfigDone(msg setupSaveConfigDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(fmt.Sprintf("Failed to save configuration: %v", msg.err))
		m.state = setupStateError
		return m, nil
	}

	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    "Configuration saved! ✓\n\nStarting the chat server... This may take a moment while the model loads.",
	})
	m.state = setupStateStartingServer
	m.loadingFrame = 0

	slog.Info("Starting server")

	// Reload config from disk to ensure consistency
	config, err := llm.LoadConfig()
	if err != nil {
		m.addWizardMessage(fmt.Sprintf("Failed to load saved configuration: %v", err))
		m.state = setupStateError
		return m, nil
	}

	// Start server
	return m, tea.Batch(
		m.startServer(config),
		loadingTick(),
	)
}

func (m setupModel) checkPersona() tea.Cmd {
	return func() tea.Msg {
		personaPath, err := agent.EnsurePersonaFile()
		if err != nil {
			// If we can't check, just skip this step
			return setupPersonaCheckDoneMsg{isDefault: false, personaPath: ""}
		}

		persona, err := os.ReadFile(personaPath)
		if err != nil {
			return setupPersonaCheckDoneMsg{isDefault: false, personaPath: ""}
		}

		// Check if it's still the default by comparing with embedded default
		defaultPersona := agent.LoadDefaultPersona()

		isDefault := strings.TrimSpace(string(persona)) == strings.TrimSpace(defaultPersona)
		return setupPersonaCheckDoneMsg{isDefault: isDefault, personaPath: personaPath}
	}
}

func (m setupModel) handlePersonaCheckDone(msg setupPersonaCheckDoneMsg) (tea.Model, tea.Cmd) {
	m.personaPath = msg.personaPath

	// Only offer customization if persona is still default (first-time setup)
	if msg.isDefault && msg.personaPath != "" {
		m.conversation = append(m.conversation, setupMessage{
			speaker: "wizard",
			text: "One more thing! Would you like to personalize your agent?\n\n" +
				"You can give it a name, set its tone, and share context about what you're working on.\n" +
				"This helps the agent understand you better.\n\n" +
				"Customize now? (yes/no, or press Enter to skip)",
		})
		m.state = setupStateWaitPersonaCustomize
		return m, nil
	}

	// Persona already customized or not available - proceed to save config
	m.addWizardMessage("Saving configuration...")
	m.state = setupStateSaving
	return m, m.saveConfig()
}

func (m setupModel) startServer(config *llm.Config) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Starting chat server")
		manager, err := startChatServer(m.ctx, config)
		slog.Info("Server start completed", "error", err)
		return setupServerStartDoneMsg{manager: manager, err: err}
	}
}

func (m setupModel) handleServerStartDone(msg setupServerStartDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(fmt.Sprintf("Failed to start chat server: %v", msg.err))
		m.state = setupStateError
		return m, nil
	}

	// Store manager in result
	if m.result == nil {
		m.result = &SetupResult{}
	}
	m.result.Manager = msg.manager

	m.conversation = append(m.conversation, setupMessage{
		speaker: "wizard",
		text:    "Server ready! ✓\n\nWhich mode would you like to start with?\n\n1. Coach - Guidance for everyday reflection, decisions, and next steps\n2. Performance Review - Structured feedback on effectiveness, habits and growth areas\n3. Analyst - Deeper psychoanalytic questioning to examine motives and patterns\n\nEnter 1, 2, or 3:",
	})
	m.state = setupStateWaitModeSelection
	return m, nil
}

func (m *setupModel) addWizardMessage(text string) {
	m.conversation = append(m.conversation, setupMessage{speaker: "wizard", text: text})
}

func (m setupModel) handleModeSelection(input string) (tea.Model, tea.Cmd) {
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > 3 {
		m.addWizardMessage("Invalid selection. Please enter 1, 2, or 3:")
		return m, nil
	}

	modes := []string{"coach", "performance-review", "analyst"}
	m.selectedMode = modes[selection-1]

	if m.result == nil {
		m.result = &SetupResult{}
	}
	m.result.SelectedMode = m.selectedMode

	m.state = setupStateDone

	return m, tea.Quit
}

func (m setupModel) handlePersonaCustomizeConfirm(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)

	// Allow empty input as "no" (skip)
	if input == "" {
		m.addWizardMessage(fmt.Sprintf(
			"No problem! You can always customize it later by editing:\n%s\n\nSaving configuration...",
			m.personaPath,
		))
		m.state = setupStateSaving
		return m, m.saveConfig()
	}

	// Validate yes/no
	if !isYesOrNo(input) {
		m.addWizardMessage("Please answer yes or no (or press Enter to skip).\n\nCustomize now? (yes/no, or press Enter to skip)")
		return m, nil
	}

	if !isYes(input) {
		m.addWizardMessage(fmt.Sprintf(
			"No problem! You can always customize it later by editing:\n%s\n\nSaving configuration...",
			m.personaPath,
		))
		m.state = setupStateSaving
		return m, m.saveConfig()
	}

	m.addWizardMessage("Great! Let's personalize your agent. I'll ask you a few quick questions.\n\nFirst, would you like to give your agent a name? (or press Enter to skip)")
	m.state = setupStateWaitPersonaName
	return m, nil
}

func (m setupModel) handlePersonaNameInput(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)
	if input != "" {
		m.personaName = input
		m.addWizardMessage(fmt.Sprintf("Nice! Your agent will be called %s.\n\nWhat tone or personality should %s have?\n(e.g., warm and encouraging, direct and practical, thoughtful and analytical)\n(or press Enter to skip)", input, input))
	} else {
		m.addWizardMessage("What tone or personality should the agent have?\n(e.g., warm and encouraging, direct and practical, thoughtful and analytical)\n(or press Enter to skip)")
	}
	m.state = setupStateWaitPersonaTone
	return m, nil
}

func (m setupModel) handlePersonaToneInput(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)
	if input != "" {
		m.personaTone = input
	}

	m.addWizardMessage("What areas of your life are you focusing on right now?\n(e.g., career transition, building better habits, managing relationships, personal projects)\n(or press Enter to skip)")
	m.state = setupStateWaitPersonaGoals
	return m, nil
}

func (m setupModel) handlePersonaGoalsInput(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)
	if input != "" {
		m.personaGoals = input
	}

	// Build the customized persona
	m.addWizardMessage("Perfect! Saving your personalized agent configuration...")
	m.state = setupStateSavingPersona
	return m, m.savePersona()
}

func (m setupModel) savePersona() tea.Cmd {
	return func() tea.Msg {
		var personaBuilder strings.Builder

		// Add name if provided
		if m.personaName != "" {
			personaBuilder.WriteString(fmt.Sprintf("Your name is %s.\n\n", m.personaName))
		}

		// Add tone/personality
		if m.personaTone != "" {
			personaBuilder.WriteString(fmt.Sprintf("You are %s.\n\n", m.personaTone))
		} else {
			personaBuilder.WriteString("You are a calm, practical AI life coach.\n\n")
		}

		// Add user context/focus areas
		if m.personaGoals != "" {
			personaBuilder.WriteString(fmt.Sprintf("Context about me: I'm currently focused on %s.\n\n", m.personaGoals))
		}

		// Add standard guidelines
		personaBuilder.WriteString("Be supportive, honest, and grounded.\n")
		personaBuilder.WriteString("Prefer concrete advice over vague motivation.\n")
		personaBuilder.WriteString("Keep responses concise unless I ask for more depth.\n")
		personaBuilder.WriteString("Ask at most one clarifying question when needed.\n")

		err := os.WriteFile(m.personaPath, []byte(personaBuilder.String()), 0o644)
		return setupPersonaSaveDoneMsg{err: err}
	}
}

func (m setupModel) handlePersonaSaveDone(msg setupPersonaSaveDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addWizardMessage(fmt.Sprintf("Failed to save persona: %v\n\nYou can manually edit it later at:\n%s\n\nSaving configuration...", msg.err, m.personaPath))
	} else {
		m.addWizardMessage("Persona saved! ✓\n\nSaving configuration...")
	}

	m.state = setupStateSaving
	return m, m.saveConfig()
}

func loadingTick() tea.Cmd {
	return tea.Tick(loadingAnimationInterval, func(time.Time) tea.Msg {
		return setupLoadingTickMsg{}
	})
}

func renderLoadingAnimation(frame int) string {
	loadingText := "Loading model"
	// Create a sweep effect through the text
	runes := []rune(loadingText)
	highlight := frame % len(runes)
	var b strings.Builder

	for i, r := range runes {
		distance := int(math.Abs(float64(i - highlight)))
		var color lipgloss.Color
		switch distance {
		case 0:
			color = lipgloss.Color("130") // Brown highlight
		case 1:
			color = lipgloss.Color("136")
		case 2:
			color = lipgloss.Color("94")
		default:
			color = lipgloss.Color("240")
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(color).Render(string(r)))
	}

	// Add animated dots
	dotsCount := (frame / loadingDotsDivisor) % loadingDotsFrames
	dots := strings.Repeat(".", dotsCount)
	spaces := strings.Repeat(" ", loadingDotsFrames-1-dotsCount)
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(dots + spaces))

	return b.String()
}

func (m setupModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Brown/warm color scheme
	wizardStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("130")).Bold(true)
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("180")).Bold(true)
	wizardTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	userTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	inputBoxStyle := lipgloss.NewStyle().
		BorderTop(true).
		BorderBottom(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(lipgloss.Color("94")).
		Padding(0, 1)
	statusBarStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("130")).
		Bold(true)

	inputHeight := 3
	statusBarHeight := 1
	chatHeight := m.height - inputHeight - statusBarHeight
	contentWidth := m.width - (chatPadding * 2) // Account for left and right padding

	var blocks []string
	for _, msg := range m.conversation {
		if msg.speaker == "wizard" {
			blocks = append(blocks, renderAgentBlock(contentWidth, wizardStyle, wizardTextStyle, "Setup Wizard", msg.text))
		} else {
			blocks = append(blocks, renderUserBlock(contentWidth, userStyle, userTextStyle, "You", msg.text))
		}
	}

	// Show loading animation if server is starting
	if m.state == setupStateStartingServer {
		contentWidth := int(float64(m.width) * messageBlockWidthRatio)
		loadingBlock := wizardTextStyle.Width(contentWidth).Render(renderLoadingAnimation(m.loadingFrame))
		content := lipgloss.NewStyle().
			Width(contentWidth).
			MarginTop(1).
			Render(loadingBlock)
		blocks = append(blocks, lipgloss.PlaceHorizontal(m.width, lipgloss.Left, content))
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
		inputText = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("Press Ctrl+C to exit")
	} else if m.input == "" {
		inputText = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).Render("█ Type your response and press Enter")
	} else {
		inputText = m.input + "█"
	}

	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("94")).Bold(true).Render("▸ ")
	inputPane := inputBoxStyle.Width(m.width).Render(prompt + inputText)

	// Status bar with mixed styles like the main app
	modeName := lipgloss.NewStyle().
		Foreground(lipgloss.Color("130")).
		Bold(true).
		Render("Setup Mode")
	hints := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render("⏎ send · ^C quit")
	statusBar := statusBarStyle.Width(m.width).Render(modeName + " | " + hints)

	return lipgloss.JoinVertical(lipgloss.Left, chatPane, inputPane, statusBar)
}

func isYes(input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes" || input == "ye" || input == "yeah"
}

func isNo(input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "n" || input == "no" || input == "nope"
}

func isYesOrNo(input string) bool {
	return isYes(input) || isNo(input)
}

// SetupResult contains the result of the setup wizard
type SetupResult struct {
	Manager      *llm.Manager
	SelectedMode string
}

func startChatServer(ctx context.Context, config *llm.Config) (*llm.Manager, error) {
	slog.Info("Creating LLM manager")
	manager, err := llm.NewManager(config)
	if err != nil {
		return nil, fmt.Errorf("create LLM manager: %w", err)
	}

	slog.Info("Starting chat server")
	if err := manager.StartChatServer(ctx); err != nil {
		return nil, fmt.Errorf("start chat server: %w", err)
	}

	return manager, nil
}
