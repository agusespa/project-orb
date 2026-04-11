package llm

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	if elapsed > gracefulStopTimeout {
		t.Errorf("stopServerLocked waited too long, elapsed: %v", elapsed)
	}
}

func TestStopServerLockedGracefullyTerminatesBeforeForceKill(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	termFile := filepath.Join(t.TempDir(), "term.log")
	readyFile := filepath.Join(t.TempDir(), "ready.log")
	ctx := context.Background()
	manager.chat.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=TestGracefulShutdownHelperProcess")
	manager.chat.cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"TERM_FILE="+termFile,
		"READY_FILE="+readyFile,
	)
	if err := manager.chat.cmd.Start(); err != nil {
		t.Fatalf("failed to start mock process: %v", err)
	}
	manager.chat.cancel = func() {}
	manager.chat.done = make(chan struct{})
	go func() {
		_ = manager.chat.cmd.Wait()
		close(manager.chat.done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(readyFile); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	manager.chat.mu.Lock()
	err = manager.stopServerLocked(&manager.chat, "chat")
	manager.chat.mu.Unlock()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("stopServerLocked returned error: %v", err)
	}

	if elapsed >= gracefulStopTimeout {
		t.Errorf("stopServerLocked should have exited during graceful shutdown, elapsed: %v", elapsed)
	}

	content, readErr := os.ReadFile(termFile)
	if readErr != nil {
		t.Fatalf("failed to read term log: %v", readErr)
	}

	if strings.TrimSpace(string(content)) != "term" {
		t.Fatalf("expected process to handle SIGTERM gracefully, got %q", string(content))
	}
}

func TestGracefulShutdownHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	termFile := os.Getenv("TERM_FILE")
	readyFile := os.Getenv("READY_FILE")
	if termFile == "" || readyFile == "" {
		os.Exit(2)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)
	if err := os.WriteFile(readyFile, []byte("ready\n"), 0o644); err != nil {
		os.Exit(4)
	}

	<-sigChan
	if err := os.WriteFile(termFile, []byte("term\n"), 0o644); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
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
	waitFunc := func(ctx context.Context, port int) error {
		<-ctx.Done()
		return ctx.Err()
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

	waitFunc := func(ctx context.Context, port int) error { return nil }

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

func TestEnsurePortAvailableRejectsUnrelatedProcess(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on test port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	err = ensurePortAvailableForServer("chat", port, "/tmp/chat.gguf")
	if err == nil {
		t.Fatal("ensurePortAvailable should fail when an unrelated process owns the port")
	}

	if !strings.Contains(err.Error(), "port") || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("unexpected error: %v", err)
	}

	testConn, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
	if dialErr != nil {
		t.Fatalf("listener should still be alive after preflight check: %v", dialErr)
	}
	_ = testConn.Close()
}

func TestEnsurePortAvailableRejectsProcessWithoutMatchingState(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on test port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	if err := writeManagedServerState(managedServerState{
		ServerType: "chat",
		PID:        os.Getpid() + 9999,
		Port:       port,
		ModelPath:  "/tmp/chat.gguf",
		StartedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("failed to write managed server state: %v", err)
	}

	err = ensurePortAvailableForServer("chat", port, "/tmp/chat.gguf")
	if err == nil {
		t.Fatal("ensurePortAvailableForServer should reject occupied ports without a matching state record")
	}

	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagedServerStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	expected := managedServerState{
		ServerType: "embedding",
		PID:        1234,
		Port:       8081,
		ModelPath:  "/models/embed.gguf",
		StartedAt:  time.Now().UTC().Round(0),
	}

	if err := writeManagedServerState(expected); err != nil {
		t.Fatalf("writeManagedServerState failed: %v", err)
	}

	actual, err := readManagedServerState("embedding")
	if err != nil {
		t.Fatalf("readManagedServerState failed: %v", err)
	}

	if actual.ServerType != expected.ServerType || actual.PID != expected.PID || actual.Port != expected.Port || actual.ModelPath != expected.ModelPath {
		t.Fatalf("unexpected managed server state: %+v", actual)
	}

	if err := removeManagedServerState("embedding"); err != nil {
		t.Fatalf("removeManagedServerState failed: %v", err)
	}

	if _, err := readManagedServerState("embedding"); !os.IsNotExist(err) {
		t.Fatalf("expected managed server state to be removed, got %v", err)
	}
}

func TestLooksLikeManagedLlamaServer(t *testing.T) {
	modelPath := "/models/chat.gguf"

	if !looksLikeManagedLlamaServer("llama-server -m /models/chat.gguf --port 8080", 8080, modelPath) {
		t.Fatal("expected exact llama-server command to be recognized")
	}

	if looksLikeManagedLlamaServer("python -m http.server 8080", 8080, modelPath) {
		t.Fatal("expected unrelated command to be rejected")
	}

	if looksLikeManagedLlamaServer("llama-server -m /models/other.gguf --port 8080", 8080, modelPath) {
		t.Fatal("expected different model path to be rejected")
	}
}

func TestTerminateOwnedProcessSendsTermBeforeKill(t *testing.T) {
	originalFindProcess := osFindProcess
	defer func() {
		osFindProcess = originalFindProcess
	}()

	handle := &stubProcessHandle{}
	osFindProcess = func(pid int) (processHandle, error) {
		return handle, nil
	}

	checks := 0
	originalStillRunning := processStillRunningFunc
	defer func() {
		processStillRunningFunc = originalStillRunning
	}()
	processStillRunningFunc = func(pid int) (bool, error) {
		checks++
		return checks == 1, nil
	}

	if err := terminateOwnedProcess(42); err != nil {
		t.Fatalf("terminateOwnedProcess returned error: %v", err)
	}

	if len(handle.signals) != 1 || handle.signals[0] != syscall.SIGTERM {
		t.Fatalf("expected a single SIGTERM before exit, got %+v", handle.signals)
	}

	if handle.killed {
		t.Fatal("did not expect force kill when process exits after SIGTERM")
	}
}

func TestTerminateOwnedProcessFallsBackToKill(t *testing.T) {
	originalFindProcess := osFindProcess
	defer func() {
		osFindProcess = originalFindProcess
	}()

	handle := &stubProcessHandle{}
	osFindProcess = func(pid int) (processHandle, error) {
		return handle, nil
	}

	originalStillRunning := processStillRunningFunc
	defer func() {
		processStillRunningFunc = originalStillRunning
	}()

	start := time.Now()
	processStillRunningFunc = func(pid int) (bool, error) {
		return time.Since(start) < gracefulStopTimeout+200*time.Millisecond, nil
	}

	if err := terminateOwnedProcess(7); err != nil {
		t.Fatalf("terminateOwnedProcess returned error: %v", err)
	}

	if len(handle.signals) == 0 || handle.signals[0] != syscall.SIGTERM {
		t.Fatalf("expected SIGTERM before fallback kill, got %+v", handle.signals)
	}

	if !handle.killed {
		t.Fatal("expected fallback kill after graceful timeout")
	}
}

func TestWaitForEmbeddingServerRespectsCancellation(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = manager.waitForEmbeddingServer(ctx, 65534)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waitForEmbeddingServer should return a cancellation error")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}

	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForEmbeddingServer returned too slowly after cancellation: %v", elapsed)
	}
}

func TestWaitForChatServerRespectsCancellation(t *testing.T) {
	config := createTestConfig(t)
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = manager.waitForChatServer(ctx, 65534)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waitForChatServer should return a cancellation error")
	}

	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}

	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForChatServer returned too slowly after cancellation: %v", elapsed)
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
	waitFunc := func(ctx context.Context, port int) error {
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

type stubProcessHandle struct {
	signals []syscall.Signal
	killed  bool
}

func (s *stubProcessHandle) Signal(sig syscall.Signal) error {
	s.signals = append(s.signals, sig)
	return nil
}

func (s *stubProcessHandle) Kill() error {
	s.killed = true
	return nil
}

func TestTerminateOwnedProcessPropagatesLookupFailure(t *testing.T) {
	originalFindProcess := osFindProcess
	defer func() {
		osFindProcess = originalFindProcess
	}()

	osFindProcess = func(pid int) (processHandle, error) {
		return nil, errors.New("boom")
	}

	if err := terminateOwnedProcess(9); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected lookup failure to be returned, got %v", err)
	}
}
