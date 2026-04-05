package llm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerShutdownStopsRunningServers(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Simulate running servers with a sleep command that we can kill
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock chat process: %v", err)
	}
	manager.chat.cancel = func() {}
	manager.chat.done = make(chan struct{})
	go func() {
		_ = manager.chat.cmd.Wait()
		close(manager.chat.done)
	}()

	manager.embedding.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.embedding.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock embedding process: %v", err)
	}
	manager.embedding.cancel = func() {}
	manager.embedding.done = make(chan struct{})
	go func() {
		_ = manager.embedding.cmd.Wait()
		close(manager.embedding.done)
	}()

	// Shutdown should not error
	err = manager.Shutdown()
	if err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}

	// Verify servers are stopped
	if manager.chat.cmd != nil {
		t.Error("chat server cmd should be nil after shutdown")
	}
	if manager.embedding.cmd != nil {
		t.Error("embedding server cmd should be nil after shutdown")
	}
}

func TestManagerShutdownWithNoRunningServers(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Shutdown with no running servers should succeed
	err = manager.Shutdown()
	if err != nil {
		t.Errorf("Shutdown with no servers returned error: %v", err)
	}
}

func TestManagerShutdownIsIdempotent(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// First shutdown
	err = manager.Shutdown()
	if err != nil {
		t.Errorf("First shutdown returned error: %v", err)
	}

	// Second shutdown should also succeed
	err = manager.Shutdown()
	if err != nil {
		t.Errorf("Second shutdown returned error: %v", err)
	}
}

func TestStopServerLockedCancelsContext(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Track if cancel was called
	cancelCalled := false
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock process: %v", err)
	}
	manager.chat.cancel = func() { cancelCalled = true }
	manager.chat.done = make(chan struct{})
	go func() {
		_ = manager.chat.cmd.Wait()
		close(manager.chat.done)
	}()

	manager.chat.mu.Lock()
	err = manager.stopServerLocked(&manager.chat, "chat")
	manager.chat.mu.Unlock()

	if err != nil {
		t.Errorf("stopServerLocked returned error: %v", err)
	}

	if !cancelCalled {
		t.Error("cancel function was not called during shutdown")
	}
}

func TestStopServerLockedWaitsForProcessExit(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	done := make(chan struct{})
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, "sleep", "0.1")
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock process: %v", err)
	}
	manager.chat.cancel = func() {}
	manager.chat.done = done

	// Close done channel after process exits
	go func() {
		_ = manager.chat.cmd.Wait()
		close(done)
	}()

	start := time.Now()
	manager.chat.mu.Lock()
	err = manager.stopServerLocked(&manager.chat, "chat")
	manager.chat.mu.Unlock()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("stopServerLocked returned error: %v", err)
	}

	// Should have waited for the done channel but not the full timeout
	if elapsed > 1*time.Second {
		t.Errorf("stopServerLocked waited too long, elapsed: %v", elapsed)
	}
}

func TestStopServerLockedTimesOutWaitingForExit(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a done channel that never closes
	done := make(chan struct{})
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock process: %v", err)
	}
	manager.chat.cancel = func() {}
	manager.chat.done = done

	start := time.Now()
	manager.chat.mu.Lock()
	err = manager.stopServerLocked(&manager.chat, "chat")
	manager.chat.mu.Unlock()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("stopServerLocked returned error: %v", err)
	}

	// Should have timed out after 2 seconds
	if elapsed < 2*time.Second {
		t.Errorf("stopServerLocked did not wait for timeout, elapsed: %v", elapsed)
	}

	if elapsed > 3*time.Second {
		t.Errorf("stopServerLocked waited too long, elapsed: %v", elapsed)
	}
}

func TestStartServerCancellationDuringStartup(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a context that we'll cancel immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before starting

	// Create a mock wait function that would block forever
	waitFunc := func(port int) error {
		<-make(chan struct{}) // Block forever
		return nil
	}

	err = manager.startServer(
		ctx,
		&manager.chat,
		"chat",
		8080,
		"/fake/model.gguf",
		[]string{"-m", "/fake/model.gguf"},
		waitFunc,
	)

	if err == nil {
		t.Error("startServer should return error when context is cancelled")
	}

	// Server should not be running
	if manager.IsChatServerRunning() {
		t.Error("chat server should not be running after cancelled startup")
	}
}

func TestStartServerFailsIfAlreadyRunning(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Simulate already running server
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock process: %v", err)
	}
	defer func() {
		_ = manager.chat.cmd.Process.Kill()
	}()

	waitFunc := func(port int) error { return nil }

	err = manager.startServer(
		ctx,
		&manager.chat,
		"chat",
		8080,
		"/fake/model.gguf",
		[]string{"-m", "/fake/model.gguf"},
		waitFunc,
	)

	if err == nil {
		t.Error("startServer should return error when server already running")
	}

	if err != nil && err.Error() != "chat server already running" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConcurrentShutdownIsSafe(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Set up mock servers
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock chat process: %v", err)
	}
	manager.chat.cancel = func() {}
	manager.chat.done = make(chan struct{})
	go func() {
		_ = manager.chat.cmd.Wait()
		close(manager.chat.done)
	}()

	manager.embedding.cmd = exec.CommandContext(ctx, "sleep", "10")
	if err := manager.embedding.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock embedding process: %v", err)
	}
	manager.embedding.cancel = func() {}
	manager.embedding.done = make(chan struct{})
	go func() {
		_ = manager.embedding.cmd.Wait()
		close(manager.embedding.done)
	}()

	// Call shutdown concurrently
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			done <- manager.Shutdown()
		}()
	}

	// Collect results
	for i := 0; i < 3; i++ {
		err := <-done
		if err != nil {
			t.Errorf("concurrent shutdown %d returned error: %v", i, err)
		}
	}
}

// Helper functions

func createTestConfig(t *testing.T) *Config {
	t.Helper()

	tmpDir := t.TempDir()
	chatModel := filepath.Join(tmpDir, "chat.gguf")
	embeddingModel := filepath.Join(tmpDir, "embedding.gguf")

	// Create dummy model files
	if err := os.WriteFile(chatModel, []byte("fake"), 0644); err != nil {
		t.Fatalf("failed to create test chat model: %v", err)
	}
	if err := os.WriteFile(embeddingModel, []byte("fake"), 0644); err != nil {
		t.Fatalf("failed to create test embedding model: %v", err)
	}

	return &Config{
		LlamaCpp: LlamaCppConfig{
			ModelsDir:      tmpDir,
			ChatModel:      "chat.gguf",
			EmbeddingModel: "embedding.gguf",
			ChatPort:       8080,
			EmbeddingPort:  8081,
		},
	}
}

// Integration-style test that verifies cancellation behavior
func TestServerStartupCancellationIntegration(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create a context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// This wait function will keep trying but the context will cancel
	waitFunc := func(port int) error {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				// Keep checking
			}
		}
	}

	err = manager.startServer(
		ctx,
		&manager.chat,
		"chat",
		8080,
		"/fake/model.gguf",
		[]string{"-m", "/fake/model.gguf"},
		waitFunc,
	)

	if err == nil {
		t.Error("startServer should return error when context times out")
	}

	// Cleanup should work
	if err := manager.Shutdown(); err != nil {
		t.Errorf("Shutdown after failed start returned error: %v", err)
	}
}
