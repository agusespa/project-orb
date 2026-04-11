package text

import (
	"fmt"
	"strings"
)

// SwitchedToMode returns a message indicating the mode was switched
func SwitchedToMode(modeName string) string {
	return fmt.Sprintf("Switched to %s mode with a fresh session", modeName)
}

// NoMatchingMode returns an error message for an invalid mode query
func NoMatchingMode(query string) string {
	return fmt.Sprintf("no matching mode for %q", query)
}

// UnsavedSessionSwitchWarning warns about discarding an unsaved session when switching modes
func UnsavedSessionSwitchWarning(targetModeName string) string {
	return fmt.Sprintf("Unsaved Analysis session will be discarded if you switch to %s. Use /wrap to save it, or switch to %s again to discard it.", targetModeName, targetModeName)
}

// UnknownCommand returns an error message for an unknown command
func UnknownCommand(command string) string {
	return fmt.Sprintf("unknown command %q", command)
}

// ModeInstructionsMissing returns an error message for a mode without instructions.
func ModeInstructionsMissing(mode string) string {
	return fmt.Sprintf("mode %q has no instructions configured", mode)
}

// InvalidModelSelection returns an error message for selecting an invalid model index.
func InvalidModelSelection(count int) string {
	return fmt.Sprintf("invalid selection. Please enter a number between 1 and %d", count)
}

// PersonaNameLine returns the persona file line for the agent's name.
func PersonaNameLine(name string) string {
	return fmt.Sprintf("Your name is %s.\n\n", name)
}

// PersonaToneLine returns the persona file line for the agent's tone.
func PersonaToneLine(tone string) string {
	return fmt.Sprintf("You are %s.\n", tone)
}

// AnalysisContext wraps the current analysis for the response prompt.
func AnalysisContext(analysis string) string {
	return "Analysis:\n" + strings.TrimSpace(analysis)
}

// ConversationSummary wraps an existing conversation summary for the summary task.
func ConversationSummary(summary string) string {
	return "Conversation summary:\n" + strings.TrimSpace(summary)
}

// ExistingConversationSummary wraps an existing summary before compaction.
func ExistingConversationSummary(summary string) string {
	return "Existing conversation summary:\n" + strings.TrimSpace(summary)
}

// RelevantPastSessionSummaries is the heading used for memory context.
func RelevantPastSessionSummaries() string {
	return "Relevant past session summaries:\n"
}

// SessionSummaryHeading renders a single saved session heading.
func SessionSummaryHeading(sessionID string) string {
	return fmt.Sprintf("Session %s:\n", sessionID)
}

// SupportingTranscriptExcerptHeading is the heading for a transcript excerpt.
func SupportingTranscriptExcerptHeading() string {
	return "Supporting transcript excerpt:\n"
}

// StartupGuidanceRoleMapping returns the role mapping prompt used to seed startup guidance.
func StartupGuidanceRoleMapping(agentName string) string {
	return fmt.Sprintf(
		"Role mapping for this task:\n- Assistant name: %s\n- Human participant: the user\n\nWrite to the user in second person. Do not address the user as %s unless the saved summary explicitly says that is the user's name.",
		agentName,
		agentName,
	)
}

// SessionWrapped returns a message indicating the session was wrapped and saved
func SessionWrapped(modeName string) string {
	return fmt.Sprintf("Wrapped and saved the %s session. Quitting...", modeName)
}

// AskForModelsDir prompts the user for the models directory path
func AskForModelsDir(defaultPath string) string {
	return fmt.Sprintf("Now let's configure your models.\nWhere do you keep your .gguf model files?\n(Enter the full path, e.g., %smodels)", defaultPath)
}

// FoundConfiguration displays the selected chat and embedding models
func FoundConfiguration(modelsDir, chatModel, embeddingModel string) string {
	return fmt.Sprintf(
		"Found your configuration:\n"+
			"  Models: %s\n"+
			"  Chat: %s\n"+
			"  Embedding: %s\n\n"+
			"Starting the chat server... This may take a moment while the model loads.",
		modelsDir, chatModel, embeddingModel,
	)
}

// FoundModels renders the list of discovered models and the follow-up chat prompt.
func FoundModels(models []string) string {
	var b strings.Builder

	b.WriteString("I found these models:\n")
	for i, model := range models {
		b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, model))
	}
	b.WriteString(AskForChatModel(len(models)))

	return b.String()
}

// InstallLlamaServerPrompt asks the user to confirm installing llama-server.
func InstallLlamaServerPrompt() string {
	return "Please answer yes or no. Would you like me to install llama-server? (yes/no)"
}

// AskForChatModel prompts the user to select a chat model
func AskForChatModel(count int) string {
	return fmt.Sprintf("\nWhich one should I use for chat? (1-%d)", count)
}

// AskForEmbeddingModel prompts the user to select an embedding model
func AskForEmbeddingModel(chatModel string, count int) string {
	return fmt.Sprintf("Great! Using %s for chat.\nWhich model should I use for embeddings? (1-%d)", chatModel, count)
}

// SelectedEmbeddingModel confirms the selected embedding model
func SelectedEmbeddingModel(embeddingModel string) string {
	return fmt.Sprintf("Perfect! Using %s for embeddings.", embeddingModel)
}

// AgentNamed confirms the agent's name
func AgentNamed(agentName string) string {
	return fmt.Sprintf("Nice! Your agent will be called %s.", agentName)
}

// AskForPersonality prompts the user to define the agent's personality
func AskForPersonality(subject string) string {
	return fmt.Sprintf("What personality should %s have?\n(default: %s)\n(press Enter to use default)", subject, DefaultPersonality)
}

// FailedToLoadConfig returns an error message for configuration loading failure
func FailedToLoadConfig(err error) string {
	return fmt.Sprintf("Failed to load saved configuration: %v", err)
}

// InstallationFailed returns an error message for installation failure
func InstallationFailed(err error) string {
	return fmt.Sprintf("Installation failed: %v\nPlease install llama.cpp manually and try again.", err)
}

// UpdateFailed returns an error message for update failure
func UpdateFailed(err error) string {
	return fmt.Sprintf("Update failed: %v\nContinuing with current version.", err)
}

// FailedToSaveConfig returns an error message for configuration save failure
func FailedToSaveConfig(err error) string {
	return fmt.Sprintf("Failed to save configuration: %v", err)
}

// FailedToScanDirectory returns an error message for directory scanning failure
func FailedToScanDirectory(err error) string {
	return fmt.Sprintf("Failed to scan directory: %v\nPlease try a different path:", err)
}

// FailedToStartServer returns an error message for server start failure
func FailedToStartServer(err error) string {
	return fmt.Sprintf("Failed to start chat server: %v", err)
}

// FailedToSetupPersona returns an error message for persona setup failure
func FailedToSetupPersona(err error) string {
	return fmt.Sprintf("Failed to set up persona customization: %v\nContinuing with the default agent.", err)
}

// PersonaSaved returns a success message with the persona file location
func PersonaSaved(personaPath string) string {
	return fmt.Sprintf("Persona saved!\n\nYour persona file is located at:\n%s\n\nYou can edit this file anytime to customize your agent's personality.", personaPath)
}

// FailedToSavePersona returns an error message for persona save failure
func FailedToSavePersona(err error, personaPath string) string {
	return fmt.Sprintf("Failed to save persona: %v\nYou can manually edit it later at:\n%s\nStarting the chat server... This may take a moment while the model loads.", err, personaPath)
}
