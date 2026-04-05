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

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	logPath, err := getLogPath()
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}

	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()
	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Always run setup wizard to confirm config and select mode
	slog.Info("Starting setup wizard...")
	sm := newSetupModel(ctx)
	p := tea.NewProgram(sm, tea.WithAltScreen(), tea.WithContext(ctx))
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Setup failed: %v\n", err)
		os.Exit(1)
	}

	// Extract result from final model
	var setupResult *SetupResult
	if model, ok := finalModel.(setupModel); ok && model.result != nil {
		setupResult = model.result
	}

	// Check if setup completed successfully
	if setupResult == nil || setupResult.Manager == nil || setupResult.SelectedMode == "" {
		fmt.Println("Setup was not completed. Exiting.")
		if setupResult != nil && setupResult.Manager != nil {
			_ = setupResult.Manager.Shutdown()
		}
		os.Exit(1)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Shutdown signal received, cleaning up...")
		if setupResult.Manager != nil {
			_ = setupResult.Manager.Shutdown()
		}
		cancel()
	}()

	slog.Info("Starting agent...", "mode", setupResult.SelectedMode)

	p = newProgram(initialModel(setupResult), ctx)
	if _, err := p.Run(); err != nil {
		fmt.Printf("failed to start UI: %v\n", err)
	}

	slog.Info("Application stopped, shutting down servers...")
	if setupResult.Manager != nil {
		if err := setupResult.Manager.Shutdown(); err != nil {
			slog.Error("Failed to shutdown servers", "error", err)
		}
	}
	slog.Info("Shutdown complete")
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
