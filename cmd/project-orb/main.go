package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	f, err := os.OpenFile("debug.log", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
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

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Shutdown signal received, cleaning up...")
		if globalManager != nil {
			_ = globalManager.Shutdown()
		}
		cancel()
	}()

	// Always run setup wizard to confirm config and select mode
	slog.Info("Starting setup wizard...")
	setupModel := newSetupModel(ctx)
	p := tea.NewProgram(setupModel, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Setup failed: %v\n", err)
		os.Exit(1)
	}

	// Check if setup completed successfully
	if globalManager == nil || globalSelectedMode == "" {
		fmt.Println("Setup was not completed. Exiting.")
		if globalManager != nil {
			_ = globalManager.Shutdown()
		}
		os.Exit(1)
	}

	slog.Info("Starting agent...", "mode", globalSelectedMode)

	p = newProgram(initialModel(), ctx)
	if _, err := p.Run(); err != nil {
		fmt.Printf("failed to start UI: %v\n", err)
	}

	slog.Info("Application stopped, shutting down servers...")
	if globalManager != nil {
		if err := globalManager.Shutdown(); err != nil {
			slog.Error("Failed to shutdown servers", "error", err)
		}
	}
	slog.Info("Shutdown complete")
}

var globalSelectedMode string
