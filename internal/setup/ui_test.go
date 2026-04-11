package setup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"project-orb/internal/text"
	"project-orb/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestHandleKeyIgnoresTypingWhenStateIsNotInteractive(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateStartingServer

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	got := updated.(Model)

	if got.input != "" {
		t.Fatalf("expected input to stay empty while loading, got %q", got.input)
	}
}

func TestAcceptsInputForWizardPromptStates(t *testing.T) {
	model := NewModel(context.Background())

	model.state = setupStateWaitModelsDir
	if !model.acceptsInput() {
		t.Fatal("expected models-dir prompt to accept input")
	}

	model.state = setupStateSaving
	if model.acceptsInput() {
		t.Fatal("expected saving state to block input")
	}
}

func TestViewMatchesWindowHeight(t *testing.T) {
	model := NewModel(context.Background())
	model.width = 80
	model.height = 24
	model.state = setupStateWaitModelsDir

	rendered := model.View()
	if lipgloss.Height(rendered) != model.height {
		t.Fatalf("expected view height %d, got %d", model.height, lipgloss.Height(rendered))
	}
}

func TestViewReservesWarningAreaHeight(t *testing.T) {
	model := NewModel(context.Background())
	model.width = 80
	model.height = 24
	model.state = setupStateWaitModelsDir

	rendered := model.View()
	lines := strings.Split(rendered, "\n")
	if len(lines) < ui.WarningAreaHeight+ui.InputHeight+ui.StatusBarHeight {
		t.Fatalf("expected enough lines to include reserved warning area, got %d", len(lines))
	}

	warningStart := len(lines) - (ui.WarningAreaHeight + ui.InputHeight + ui.StatusBarHeight)
	warningLines := lines[warningStart : warningStart+ui.WarningAreaHeight]
	for i, line := range warningLines {
		if strings.TrimSpace(line) != "" {
			t.Fatalf("expected reserved warning line %d to be blank, got %q", i, line)
		}
	}
}

func TestViewEmbedsLoadingIntoLatestWizardMessage(t *testing.T) {
	model := NewModel(context.Background())
	model.width = 80
	model.height = 24
	model.state = setupStateCheckInstall

	rendered := model.View()

	if count := strings.Count(rendered, "WIZARD"); count != 1 {
		t.Fatalf("expected loading state to reuse the greeting block, got %d wizard blocks", count)
	}

	if !strings.Contains(rendered, text.SetupGreeting[:len("Hello! I'm your setup wizard.")]) {
		t.Fatal("expected greeting text to remain visible")
	}

	if !strings.Contains(rendered, "Checking your system configuration") {
		t.Fatal("expected loading text to be shown inside the greeting block")
	}
}

func TestWindowSizeInitializesWizardViewport(t *testing.T) {
	model := NewModel(context.Background())

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := updated.(Model)

	if !got.viewport.Ready() {
		t.Fatal("expected viewport to be initialized")
	}
	if got.viewport.Width() != ui.ChatContentWidth(80) {
		t.Fatalf("expected viewport width %d, got %d", ui.ChatContentWidth(80), got.viewport.Width())
	}
	if got.viewport.Height() != ui.ChatPaneHeight(24, 0) {
		t.Fatalf("expected viewport height %d, got %d", ui.ChatPaneHeight(24, 0), got.viewport.Height())
	}
}

func TestMouseWheelScrollsWizardConversation(t *testing.T) {
	model := NewModel(context.Background())
	model.conversation = nil
	for i := 0; i < 12; i++ {
		model.conversation = append(model.conversation, setupMessage{
			speaker: "wizard",
			text:    strings.Repeat("This is a long wizard message that should wrap and scroll. ", 4),
		})
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	model = updated.(Model)
	model.viewport.GotoBottom()

	if model.viewport.YOffset() == 0 {
		t.Fatal("expected wizard content to exceed the viewport before scrolling")
	}

	updated, _ = model.Update(tea.MouseMsg{
		X:      5,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	got := updated.(Model)

	if got.viewport.YOffset() >= model.viewport.YOffset() {
		t.Fatalf("expected wheel up to scroll upward, got offset %d from %d", got.viewport.YOffset(), model.viewport.YOffset())
	}
}

func TestWizardViewUsesViewportContent(t *testing.T) {
	model := NewModel(context.Background())
	model.conversation = nil
	for i := 0; i < 10; i++ {
		model.conversation = append(model.conversation, setupMessage{
			speaker: "wizard",
			text:    "Wizard line " + strings.Repeat("wrapped ", 8),
		})
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	model = updated.(Model)
	model.viewport.GotoBottom()

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "Wizard line") {
		t.Fatal("expected viewport content to appear in the rendered view")
	}
}

func TestHandleConfigLoadedPrefillsModelsDirWithHomePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	model := NewModel(context.Background())
	updated, _ := model.handleConfigLoaded(setupConfigLoadedMsg{})
	got := updated.(Model)

	wantPrefix := got.wizard.DefaultModelsDirInput()
	if got.state != setupStateWaitModelsDir {
		t.Fatalf("expected models-dir state, got %v", got.state)
	}
	if got.input != wantPrefix {
		t.Fatalf("expected input %q, got %q", wantPrefix, got.input)
	}
	if len(got.conversation) == 0 {
		t.Fatal("expected wizard prompt to be added")
	}
	if !strings.Contains(got.conversation[len(got.conversation)-1].text, wantPrefix+"models") {
		t.Fatalf("expected prompt to include example path rooted in %q", wantPrefix)
	}
}

func TestHintsCommandOpensHintsOverlayInSetup(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateWaitModelsDir
	model.input = "/hints"

	updated, _ := model.handleEnter()
	got := updated.(Model)

	if !got.hintsOverlay {
		t.Fatal("expected /hints to open setup hints overlay")
	}
	if got.input != "" {
		t.Fatalf("expected input to be cleared, got %q", got.input)
	}
}

func TestEscClosesHintsOverlayInSetup(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateWaitModelsDir
	model.hintsOverlay = true

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if got.hintsOverlay {
		t.Fatal("expected esc to close setup hints overlay")
	}
}

func TestSetupCtrlCWarnsBeforeShutdown(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateWaitModelsDir

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected first Ctrl+C to warn instead of quitting")
	}
	if !got.pendingQuit {
		t.Fatal("expected pendingQuit to be set after first Ctrl+C")
	}
	if got.statusMessage != text.ShutdownWarning {
		t.Fatalf("expected shutdown warning, got %q", got.statusMessage)
	}
}

func TestSetupSecondCtrlCStartsShutdownAndQuitsOnCompletion(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateWaitModelsDir

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	warned := updated.(Model)

	updated, cmd := warned.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected second Ctrl+C to start shutdown")
	}
	if !got.shuttingDown {
		t.Fatal("expected shuttingDown to be true")
	}
	if got.statusMessage != text.ShuttingDown {
		t.Fatalf("expected shutdown status message, got %q", got.statusMessage)
	}

	msg := cmd()
	shutdownMsg, ok := msg.(setupShutdownCompleteMsg)
	if !ok {
		t.Fatalf("expected setupShutdownCompleteMsg, got %T", msg)
	}
	if shutdownMsg.err != nil {
		t.Fatalf("expected shutdown to succeed, got %v", shutdownMsg.err)
	}

	next, quitCmd := got.Update(shutdownMsg)
	_ = next.(Model)
	if quitCmd == nil {
		t.Fatal("expected shutdown completion to trigger quit")
	}
	if quitMsg := quitCmd(); quitMsg == nil {
		t.Fatal("expected quit command to return a message")
	} else if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestSetupShutdownErrorDoesNotQuit(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateWaitModelsDir

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	warned := updated.(Model)

	updated, _ = warned.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	inShutdown := updated.(Model)

	next, quitCmd := inShutdown.Update(setupShutdownCompleteMsg{err: errors.New("boom")})
	got := next.(Model)
	if quitCmd != nil {
		t.Fatal("expected no quit command when shutdown fails")
	}
	if got.shuttingDown {
		t.Fatal("expected shuttingDown to be false after failed shutdown")
	}
	if !strings.Contains(got.statusMessage, text.ShutdownFailed) {
		t.Fatalf("expected shutdown failure status message, got %q", got.statusMessage)
	}
}

func TestSyncSlashCommandUIInSetupHidesHintsForNonMatchingToken(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateWaitModelsDir
	model.input = "/hintssss"
	model.hintsOverlay = true

	model.syncSlashCommandUI()

	if model.hintsOverlay {
		t.Fatal("expected setup hints overlay to close for non-matching command token")
	}
}

func TestHandleModelsDirKeepsTypedPathOnValidationError(t *testing.T) {
	model := NewModel(context.Background())
	input := filepath.Join(t.TempDir(), "missing")

	updated, _ := model.handleModelsDir(input)
	got := updated.(Model)

	if got.input != input {
		t.Fatalf("expected invalid path to stay in input, got %q", got.input)
	}
	if len(got.conversation) == 0 || !strings.Contains(got.conversation[len(got.conversation)-1].text, "Please try again:") {
		t.Fatal("expected retry prompt after invalid path")
	}
}

func TestConfigSaveSuccessPromptsPersonaSetupAfterSaving(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateSaving
	model.needsPersonaSetup = true

	updated, cmd := model.Update(setupConfigSaveDoneMsg{})
	got := updated.(Model)

	if got.state != setupStateWaitPersonaName {
		t.Fatalf("expected state %v, got %v", setupStateWaitPersonaName, got.state)
	}
	if len(got.conversation) == 0 {
		t.Fatal("expected follow-up prompt")
	}
	if len(got.conversation) < 2 {
		t.Fatal("expected configuration confirmation and personalization prompt")
	}
	saved := got.conversation[len(got.conversation)-2].text
	last := got.conversation[len(got.conversation)-1].text
	if saved != "Configuration saved!" {
		t.Fatalf("expected configuration-saved confirmation, got %q", saved)
	}
	if !strings.Contains(last, "Let's personalize your agent!") {
		t.Fatal("expected personalization prompt after configuration is saved")
	}
	if strings.Contains(last, "Saving configuration") {
		t.Fatal("did not expect transient saving message")
	}
	if cmd != nil {
		t.Fatal("expected no follow-up command when waiting for persona input")
	}
}

func TestContinueWithSavedConfigTransitionsToStartingServer(t *testing.T) {
	model := NewModel(context.Background())
	model.state = setupStateSaving

	updated, cmd := model.continueWithSavedConfig("")
	got := updated.(Model)

	if got.state != setupStateStartingServer {
		t.Fatalf("expected state %v, got %v", setupStateStartingServer, got.state)
	}
	if cmd == nil {
		t.Fatal("expected server start command batch")
	}
}

func TestHandleServerStartDoneRendersModePromptAndEnablesModeSelection(t *testing.T) {
	model := NewModel(context.Background())
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	model.state = setupStateStartingServer
	model.loadingText = ui.LoadingModelText
	model.updateViewportContent()

	updated, _ = model.handleServerStartDone(setupServerStartDoneMsg{})
	got := updated.(Model)

	if got.state != setupStateWaitModeSelection {
		t.Fatalf("expected state %v, got %v", setupStateWaitModeSelection, got.state)
	}

	rendered := ansi.Strip(got.View())
	if !strings.Contains(rendered, "Which mode would you like to start with?") {
		t.Fatal("expected mode selection prompt to be visible after loading")
	}
	if strings.Contains(rendered, ui.LoadingModelText) {
		t.Fatal("did not expect loading text after transitioning to mode selection")
	}
}
