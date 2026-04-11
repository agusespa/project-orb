package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"project-orb/internal/agent"
	"project-orb/internal/paths"
	"project-orb/internal/setup"
	"project-orb/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		logFatalError(err)
		os.Exit(1)
	}
}

var loggingConfigured bool

func logFatalError(err error) {
	if loggingConfigured {
		slog.Error("Application exited with error", "error", err)
		return
	}

	logPath, pathErr := getLogPath()
	if pathErr != nil {
		return
	}

	f, openErr := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if openErr != nil {
		return
	}
	defer f.Close()

	_, _ = fmt.Fprintf(f, "level=ERROR msg=%q error=%q\n", "Application exited with error", err.Error())
}

func run() error {
	if err := setupLogging(); err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupResult, err := runSetupWizard(ctx)
	if err != nil {
		return fmt.Errorf("setup wizard: %w", err)
	}
	shutdown := onceShutdown(setupResult.Manager)
	defer func() { _ = shutdown() }()

	cleanupSignals := setupSignalHandler(ctx, cancel, shutdown)
	defer cleanupSignals()

	slog.Info("Starting application...", "mode", setupResult.SelectedMode)

	m, err := initialModel(setupResult, shutdown)
	if err != nil {
		return fmt.Errorf("initialize model: %w", err)
	}

	p := newProgram(m, ctx)
	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("run UI: %w", err)
	}

	slog.Info("Application stopped")
	return nil
}

func newProgram(m ui.Model, ctx context.Context) *tea.Program {
	m.SetShutdownCtx(ctx)
	return tea.NewProgram(m, bubbleTeaProgramOptions(ctx)...)
}

func bubbleTeaProgramOptions(ctx context.Context) []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	}
}

func initialModel(setupResult *setup.Result, shutdown func() error) (ui.Model, error) {
	mode, found := agent.FindMode(setupResult.SelectedMode)
	if !found {
		return ui.Model{}, fmt.Errorf("mode not found: %q", setupResult.SelectedMode)
	}

	agentName, err := agent.LoadAgentName()
	if err != nil {
		return ui.Model{}, fmt.Errorf("load agent name: %w", err)
	}

	client, err := agent.NewClient(agent.DefaultClientConfig())
	if err != nil {
		return ui.Model{}, fmt.Errorf("create client: %w", err)
	}

	sessionStore, err := agent.NewFileSessionStore()
	if err != nil {
		return ui.Model{}, fmt.Errorf("create session store: %w", err)
	}

	initialSession := agent.NewSessionContext()
	statusMessage := ""
	if mode.ID == agent.ModeAnalyst {
		service, err := agent.NewService(client, mode)
		if err != nil {
			return ui.Model{}, fmt.Errorf("create initial analysis service: %w", err)
		}
		service.SetSessionStore(sessionStore)

		session, _, err := service.LoadSession(context.Background())
		if err != nil {
			return ui.Model{}, fmt.Errorf("load saved analysis session: %w", err)
		}
		initialSession = session
	}

	model, err := ui.NewModel(ui.ModelDependencies{
		Client:         client,
		CurrentMode:    mode,
		AgentName:      agentName,
		InitialSession: initialSession,
		SessionStore:   sessionStore,
		StatusMessage:  statusMessage,
		Shutdown:       shutdown,
	})
	if err != nil {
		return ui.Model{}, fmt.Errorf("create ui model: %w", err)
	}

	return model, nil
}

func setupLogging() error {
	logPath, err := getLogPath()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)
	loggingConfigured = true
	return nil
}

func runSetupWizard(ctx context.Context) (*setup.Result, error) {
	slog.Info("Starting setup wizard")

	sm := setup.NewModel(ctx)
	p := tea.NewProgram(sm, bubbleTeaProgramOptions(ctx)...)

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	model, ok := finalModel.(setup.Model)
	if !ok {
		return nil, fmt.Errorf("unexpected model type")
	}

	result := model.Result()

	// If interrupted (Ctrl+C), clean up any started manager
	if result != nil && result.Manager != nil {
		if result.SelectedMode == "" {
			// Setup was interrupted, clean up
			slog.Info("Setup interrupted, cleaning up")
			_ = shutdownManager(result.Manager)
			return nil, fmt.Errorf("setup incomplete")
		}
	}

	if result == nil || result.Manager == nil || result.SelectedMode == "" {
		return nil, fmt.Errorf("setup incomplete")
	}

	return result, nil
}

func setupSignalHandler(ctx context.Context, cancel context.CancelFunc, shutdown func() error) func() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	cleanup := setupSignalHandlerWithChannel(ctx, cancel, shutdown, sigChan)

	return func() {
		signal.Stop(sigChan)
		cleanup()
	}
}

func setupSignalHandlerWithChannel(ctx context.Context, cancel context.CancelFunc, shutdown func() error, sigChan <-chan os.Signal) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-sigChan:
			slog.Info("Shutdown signal received")
			if shutdown != nil {
				_ = shutdown()
			}
			cancel()
		}
	}()

	return func() {
		close(done)
	}
}

func onceShutdown(manager interface{ Shutdown() error }) func() error {
	var once sync.Once
	var onceErr error

	return func() error {
		once.Do(func() {
			onceErr = shutdownManager(manager)
		})
		return onceErr
	}
}

func shutdownManager(manager interface{ Shutdown() error }) error {
	if manager == nil {
		return nil
	}

	slog.Info("Shutting down servers")
	if err := manager.Shutdown(); err != nil {
		slog.Error("Failed to shutdown servers", "error", err)
		return err
	}
	return nil
}

func getLogPath() (string, error) {
	logPath, err := paths.DebugLogPath()
	if err != nil {
		return "", fmt.Errorf("resolve log directory: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", fmt.Errorf("create log directory %s: %w", filepath.Dir(logPath), err)
	}

	return logPath, nil
}
