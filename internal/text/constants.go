package text

// DefaultPersonality is the default personality trait for the agent
const DefaultPersonality = "calm, thoughtful, and supportive"
const DefaultAgentName = "Agent"
const DefaultAssistantName = "the assistant"

// Built-in mode labels and descriptions.
const (
	ModeSetupName              = "Setup"
	ModeSetupDescription       = "Guided configuration flow for installing dependencies and choosing models."
	ModeCoachName              = "Coach"
	ModeCoachDescription       = "Guidance for everyday reflection, decisions, and next steps."
	ModePerformanceName        = "Performance Review"
	ModePerformanceDescription = "Structured feedback on effectiveness, habits and growth areas."
	ModeAnalysisName           = "Analysis"
	ModeAnalysisDescription    = "Deeper analysis to examine motives and patterns."
)

// Generic UI labels and loading text.
const (
	ThinkingText                = "Thinking..."
	LoadingModelText            = "Loading model..."
	LoadingMemoryText           = "Loading memory..."
	CheckingSystemConfiguration = "Checking your system configuration..."
	LoadingPlaceholder          = "Loading..."
	InitializingPlaceholder     = "Initializing..."
	UserLabel                   = "You"
	WizardLabel                 = "Wizard"
	SetupModeLabel              = "Setup Mode"
	PressCtrlCToExit            = "Press Ctrl+C to exit"
	TypeYourMessagePrompt       = "Type your message and press Enter"
	TypeYourResponsePrompt      = "Type your response and press Enter"
	DefaultAgentSubject         = "the agent"
	SetupStatusHints            = "⏎ send · /hints · ^C quit"
	TryAgainPrompt              = "Please try again:"
	SelectModeTitle             = "Select Mode"
	SelectModeHint              = "↑↓ move · ⏎ switch · esc cancel"
	NoMatchingModes             = "No matching modes"
	HintsTitle                  = "Hints"
	HintsCloseHint              = "esc close"
	HintsClosePanel             = "esc             close this panel"
	HintsScrollConversation     = "wheel           scroll conversation"
	SetupHintsShowHelp          = "/hints          show this help panel"
	SetupHintsSend              = "⏎ send          submit current answer"
	SetupHintsQuit              = "^C              quit setup"
	PersonaEmptyError           = "persona is empty"
	SessionUserPrefix           = "User: "
	SessionAssistantPrefix      = "Assistant: "
	NoModelsAvailable           = "no models available"
	SessionIDRequired           = "session_id is required"
	ContextLabel                = "ctx "
	ErrorPrefix                 = "Error: "
)

// Shutdown messages
const (
	ShutdownWarning = "Press Ctrl+C again for shutdown"
	ShuttingDown    = "Shutting down..."
	ShutdownFailed  = "Graceful shutdown failed - press Ctrl+C to retry, or terminate the terminal"
)

// Session messages
const (
	CannotWrapWhileStreaming = "cannot wrap the session while a response is in progress"
	CannotWrapInThisMode     = "this mode does not support /wrap because sessions are not persisted"
	UnsavedSessionQuitMsg    = "Unsaved session will be discarded. Use /wrap to save it, or press Ctrl+C again to quit without saving."
)

// Setup wizard messages
const (
	SetupGreeting                    = "Hello! I'm your setup wizard. I'll help you get everything configured to start using the app."
	LlamaServerNotInstalled          = "I noticed llama-server isn't installed on your system. I can install it via Homebrew for you. Would you like me to install it? (yes/no)"
	LlamaServerOutdated              = "I noticed there's a newer version of llama.cpp available via Homebrew. Would you like to update it? (yes/no)"
	UpgradeLlamaCppPrompt            = "Please answer yes or no.\nWould you like to update llama.cpp? (yes/no)"
	PleaseAnswerYesNo                = "Please answer yes or no."
	LlamaServerRequired              = "I understand. Unfortunately, llama-server is required to run this application. Please install it manually and try again."
	InstallingLlamaCpp               = "Great! Installing llama.cpp via Homebrew... This may take a few minutes."
	UpdatingLlamaCpp                 = "Updating llama.cpp via Homebrew..."
	InstallationComplete             = "Installation complete!"
	UpdateComplete                   = "Update complete!"
	ContinueWithCurrentVersion       = "No problem, we'll continue with the current version."
	ConfigurationSaved               = "Configuration saved!"
	PersonalizeAgent                 = "Let's personalize your agent!\n\nWhat would you like to name your agent? (press Enter to skip)"
	ServerReady                      = "Server ready! \nWhich mode would you like to start with?\n1. Coach - Guidance for everyday reflection, decisions, and next steps\n2. Performance Review - Structured feedback on effectiveness, habits and growth areas\n3. Analysis - Deeper analysis to examine motives and patterns\n\nEnter 1, 2, or 3:"
	InvalidModeSelection             = "Invalid selection. Please enter 1, 2, or 3:"
	NoGgufFilesFound                 = "No .gguf files found in that directory. Please add model files or try a different path:"
	StartingServer                   = "Starting the chat server... This may take a moment while the model loads."
	CoachWelcomeMessage              = "Welcome. What situation, decision, or tension feels most significant right now? We will work through it together."
	PerformanceReviewWelcomeMessage  = "Welcome. We can look clearly at what is working, where you are getting stuck, and what would meaningfully improve your effectiveness."
	AnalysisWelcomeMessage           = "Welcome. This space is for slow, honest analysis. We can look directly at patterns, tensions, and the meanings underneath what is happening."
	AnalysisFreshStartMessage        = "We do not have a saved session summary yet, so we can start wherever feels most significant right now."
	AnalysisReturningFallbackMessage = "I reviewed the last saved summary. We can pick up from a recurring pattern, an unresolved tension, or a question that still feels unfinished."
)
