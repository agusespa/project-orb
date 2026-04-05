package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"project-orb/internal/agent"
	"project-orb/internal/setup"
	"project-orb/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
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
	defer shutdownManager(setupResult.Manager)

	setupSignalHandler(ctx, cancel, setupResult.Manager)

	slog.Info("Starting application...", "mode", setupResult.SelectedMode)

	m, err := initialModel(setupResult)
	if err != nil {
		return fmt.Errorf("initialize model: %w", err)
	}

	p := newProgram(m, ctx)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run UI: %w", err)
	}

	slog.Info("Application stopped")
	return nil
}

func newProgram(m ui.Model, ctx context.Context) *tea.Program {
	m.SetShutdownCtx(ctx)
	return tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
}

func initialModel(setupResult *setup.Result) (ui.Model, error) {
	mode, found := agent.FindMode(setupResult.SelectedMode)
	if !found {
		return ui.Model{}, fmt.Errorf("mode not found: %q", setupResult.SelectedMode)
	}

	personaPath, err := agent.EnsurePersonaFile()
	if err != nil {
		return ui.Model{}, fmt.Errorf("ensure persona file: %w", err)
	}

	agentName, err := agent.LoadAgentName()
	if err != nil {
		return ui.Model{}, fmt.Errorf("load agent name: %w", err)
	}

	client, err := agent.NewClient(agent.DefaultClientConfig())
	if err != nil {
		return ui.Model{}, fmt.Errorf("create client: %w", err)
	}

	return ui.NewModel(ui.ModelDependencies{
		RunnerFactory: newRunnerFactory(client),
		CurrentMode:   mode,
		AgentName:     agentName,
		PersonaPath:   personaPath,
	}), nil
}

func newRunnerFactory(client *agent.Client) ui.RunnerFactory {
	return func(mode agent.Mode) (ui.StreamRunner, error) {
		service, err := agent.NewService(client, mode)
		if err != nil {
			return nil, err
		}

		return ui.AgentRunner{Service: service}, nil
	}
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
	return nil
}

func runSetupWizard(ctx context.Context) (*setup.Result, error) {
	slog.Info("Starting setup wizard")

	sm := setup.NewModel(ctx)
	p := tea.NewProgram(sm, tea.WithAltScreen(), tea.WithContext(ctx))

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
			shutdownManager(result.Manager)
			return nil, fmt.Errorf("setup incomplete")
		}
	}

	if result == nil || result.Manager == nil || result.SelectedMode == "" {
		return nil, fmt.Errorf("setup incomplete")
	}

	return result, nil
}

func setupSignalHandler(ctx context.Context, cancel context.CancelFunc, manager interface{ Shutdown() error }) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Shutdown signal received")
		shutdownManager(manager)
		cancel()
	}()
}

func shutdownManager(manager interface{ Shutdown() error }) {
	if manager == nil {
		return
	}

	slog.Info("Shutting down servers")
	if err := manager.Shutdown(); err != nil {
		slog.Error("Failed to shutdown servers", "error", err)
	}
}

func getLogPath() (string, error) {
	logDir, err := resolveAppDataDir()
	if err != nil {
		return "", fmt.Errorf("resolve log directory: %w", err)
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create log directory %s: %w", logDir, err)
	}

	return filepath.Join(logDir, "debug.log"), nil
}

func resolveAppDataDir() (string, error) {
	const appName = "project-orb"

	if xdgDataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}

	return filepath.Join(homeDir, ".local", "share", appName), nil
}
