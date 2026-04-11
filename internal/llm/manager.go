package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"project-orb/internal/paths"
)

const (
	healthCheckInterval = 500 * time.Millisecond
	serverStartTimeout  = 5 * time.Minute
	processKillWaitTime = 100 * time.Millisecond
	gracefulStopTimeout = 2 * time.Second
)

type managedServerState struct {
	ServerType string    `json:"server_type"`
	PID        int       `json:"pid"`
	Port       int       `json:"port"`
	ModelPath  string    `json:"model_path"`
	StartedAt  time.Time `json:"started_at"`
}

func ensurePortAvailableForServer(serverType string, port int, modelPath string) error {
	state, stateErr := readManagedServerState(serverType)

	pids, err := pidsListeningOnPort(port)
	if err != nil {
		slog.Warn("Failed to inspect port usage; proceeding without preflight cleanup", "port", port, "error", err)
		return nil
	}

	if len(pids) == 0 {
		if stateErr == nil {
			_ = removeManagedServerState(serverType)
		}
		return nil
	}

	for _, pid := range pids {
		command, err := commandLineForPID(pid)
		if err != nil {
			return fmt.Errorf("inspect process on port %d (pid %d): %w", port, pid, err)
		}

		if !isOwnedManagedServer(state, stateErr, serverType, pid, port, modelPath, command) {
			return fmt.Errorf("port %d already in use by pid %d: %s", port, pid, command)
		}

		if err := terminateOwnedProcess(pid); err != nil {
			return fmt.Errorf("stop existing managed server on port %d (pid %d): %w", port, pid, err)
		}
	}

	if stateErr == nil {
		_ = removeManagedServerState(serverType)
	}

	return nil
}

func isOwnedManagedServer(state *managedServerState, stateErr error, serverType string, pid int, port int, modelPath string, command string) bool {
	if stateErr != nil || state == nil {
		return false
	}

	if state.ServerType != serverType || state.PID != pid || state.Port != port || state.ModelPath != modelPath {
		return false
	}

	return looksLikeManagedLlamaServer(command, port, modelPath)
}

func pidsListeningOnPort(port int) ([]int, error) {
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var pids []int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("parse pid %q: %w", line, err)
		}
		pids = append(pids, pid)
	}

	return pids, nil
}

func commandLineForPID(pid int) (string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func looksLikeManagedLlamaServer(command string, port int, modelPath string) bool {
	if command == "" {
		return false
	}

	portFlag := fmt.Sprintf("--port %d", port)
	shortPortFlag := fmt.Sprintf("--port=%d", port)
	modelFlag := "-m " + modelPath
	shortModelFlag := "-m=" + modelPath

	return strings.Contains(command, "llama-server") &&
		(strings.Contains(command, portFlag) || strings.Contains(command, shortPortFlag)) &&
		(strings.Contains(command, modelFlag) || strings.Contains(command, shortModelFlag))
}

func terminateOwnedProcess(pid int) error {
	process, err := osFindProcess(pid)
	if err != nil {
		return err
	}

	slog.Info("Stopping existing managed server on occupied port", "pid", pid)

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(gracefulStopTimeout)
	for time.Now().Before(deadline) {
		running, err := processStillRunningFunc(pid)
		if err != nil {
			return err
		}
		if !running {
			time.Sleep(processKillWaitTime)
			return nil
		}
		time.Sleep(processKillWaitTime)
	}

	slog.Warn("Existing managed server did not exit after SIGTERM; forcing kill", "pid", pid)
	if err := process.Kill(); err != nil {
		return err
	}

	deadline = time.Now().Add(gracefulStopTimeout)
	for time.Now().Before(deadline) {
		running, err := processStillRunningFunc(pid)
		if err != nil {
			return err
		}
		if !running {
			time.Sleep(processKillWaitTime)
			return nil
		}
		time.Sleep(processKillWaitTime)
	}

	return fmt.Errorf("process %d still running after forced kill", pid)
}

var osFindProcess = func(pid int) (processHandle, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil, err
	}
	return processWrapper{process: process}, nil
}

var processStillRunningFunc = processStillRunning

type processHandle interface {
	Signal(sig syscall.Signal) error
	Kill() error
}

type processWrapper struct {
	process *os.Process
}

func (p processWrapper) Signal(sig syscall.Signal) error {
	return p.process.Signal(sig)
}

func (p processWrapper) Kill() error {
	return p.process.Kill()
}

func processStillRunning(pid int) (bool, error) {
	cmd := exec.Command("kill", "-0", strconv.Itoa(pid))
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}

	return false, err
}

func managedServerStatePath(serverType string) (string, error) {
	appDataDir, err := resolveLLMAppDataDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(appDataDir, "servers", serverType+".json"), nil
}

func resolveLLMAppDataDir() (string, error) {
	return paths.DataDir()
}

func writeManagedServerState(state managedServerState) error {
	path, err := managedServerStatePath(state.ServerType)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create managed server state directory: %w", err)
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal managed server state: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write managed server state: %w", err)
	}

	return nil
}

func readManagedServerState(serverType string) (*managedServerState, error) {
	path, err := managedServerStatePath(serverType)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state managedServerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse managed server state: %w", err)
	}

	return &state, nil
}

func removeManagedServerState(serverType string) error {
	path, err := managedServerStatePath(serverType)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove managed server state: %w", err)
	}

	return nil
}

type serverInstance struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	mu         sync.Mutex
	done       chan struct{}
	serverType string
	modelPath  string
	port       int
}

type Manager struct {
	config *Config

	chat      serverInstance
	embedding serverInstance
}

func NewManager(config *Config) (*Manager, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Manager{
		config: config,
	}, nil
}

func (m *Manager) StartChatServer(ctx context.Context) error {
	args := []string{
		"-m", m.config.ChatModelPath(),
		"--port", fmt.Sprintf("%d", m.config.LlamaCpp.ChatPort),
		"--host", "127.0.0.1",
		"-c", "32768", // context size
		"-n", "8192", // max tokens to predict
		"-ngl", "99", // GPU layers (offload all to Metal on Apple Silicon)
		"-b", "2048", // batch size for prompt processing
		"-ub", "1024", // physical batch size
		"--temp", "0.7", // optimal for Qwen 3.5 general conversation
		"--top-p", "0.80", // nucleus sampling for non-thinking general tasks
		"--top-k", "20", // limit to top 20 tokens
		"--min-p", "0.00", // minimum probability threshold
		"-np", "1", // single parallel slot
		"-fa", "auto", // Flash Attention for Apple Silicon optimization
		"--jinja", // enable jinja2 template support
		"--chat-template-kwargs", `{"enable_thinking":false}`,
	}

	return m.startServer(
		ctx,
		&m.chat,
		"chat",
		m.config.LlamaCpp.ChatPort,
		m.config.ChatModelPath(),
		args,
		func(ctx context.Context, port int) error { return m.waitForChatServer(ctx, port) },
	)
}

func (m *Manager) StartEmbeddingServer(ctx context.Context) error {
	args := []string{
		"-m", m.config.EmbeddingModelPath(),
		"--port", fmt.Sprintf("%d", m.config.LlamaCpp.EmbeddingPort),
		"--host", "127.0.0.1",
		"--embedding",
		"-ngl", "99",
	}

	return m.startServer(
		ctx,
		&m.embedding,
		"embedding",
		m.config.LlamaCpp.EmbeddingPort,
		m.config.EmbeddingModelPath(),
		args,
		func(ctx context.Context, port int) error { return m.waitForEmbeddingServer(ctx, port) },
	)
}

func (m *Manager) startServer(
	ctx context.Context,
	instance *serverInstance,
	serverType string,
	port int,
	modelPath string,
	args []string,
	waitFunc func(context.Context, int) error,
) error {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.cmd != nil {
		return fmt.Errorf("%s server already running", serverType)
	}

	instance.serverType = serverType
	instance.modelPath = modelPath
	instance.port = port

	if err := ensurePortAvailableForServer(serverType, port, modelPath); err != nil {
		return err
	}

	slog.Info("Starting server", "type", serverType, "model", modelPath, "port", port)

	cmdCtx, cancel := context.WithCancel(ctx)
	instance.cancel = cancel
	instance.done = make(chan struct{})

	cmd := exec.CommandContext(cmdCtx, "llama-server", args...)

	slog.Info("Executing command", "cmd", cmd.String())

	if err := cmd.Start(); err != nil {
		cancel()
		close(instance.done)
		instance.cancel = nil
		instance.done = nil
		return fmt.Errorf("start %s server: %w", serverType, err)
	}

	instance.cmd = cmd

	if err := writeManagedServerState(managedServerState{
		ServerType: serverType,
		PID:        cmd.Process.Pid,
		Port:       port,
		ModelPath:  modelPath,
		StartedAt:  time.Now().UTC(),
	}); err != nil {
		_ = m.stopServerLocked(instance, serverType)
		return fmt.Errorf("record %s server state: %w", serverType, err)
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, serverStartTimeout)
	defer waitCancel()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- waitFunc(waitCtx, port)
	}()

	select {
	case err := <-waitDone:
		if err != nil {
			_ = m.stopServerLocked(instance, serverType)
			if waitCtx.Err() != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("%s server startup cancelled", serverType)
				}
				return fmt.Errorf("%s server startup timeout after %v", serverType, serverStartTimeout)
			}
			return fmt.Errorf("%s server failed to start: %w", serverType, err)
		}
	case <-waitCtx.Done():
		_ = m.stopServerLocked(instance, serverType)
		if ctx.Err() != nil {
			// Parent context was cancelled (e.g., user pressed Ctrl+C)
			return fmt.Errorf("%s server startup cancelled", serverType)
		}
		return fmt.Errorf("%s server startup timeout after %v", serverType, serverStartTimeout)
	}

	slog.Info("Server ready", "type", serverType, "port", port)

	go func() {
		defer close(instance.done)
		if err := cmd.Wait(); err != nil && cmdCtx.Err() == nil {
			slog.Error("Server exited unexpectedly", "type", serverType, "error", err)
		}
	}()

	return nil
}

func (m *Manager) StopChatServer() error {
	m.chat.mu.Lock()
	defer m.chat.mu.Unlock()
	return m.stopServerLocked(&m.chat, "chat")
}

func (m *Manager) StopEmbeddingServer() error {
	m.embedding.mu.Lock()
	defer m.embedding.mu.Unlock()
	return m.stopServerLocked(&m.embedding, "embedding")
}

func (m *Manager) stopServerLocked(instance *serverInstance, serverType string) error {
	if instance.cmd == nil {
		slog.Info("Server already stopped or not started", "type", serverType)
		return nil
	}

	if instance.cmd.Process != nil {
		slog.Info("Stopping server", "type", serverType, "pid", instance.cmd.Process.Pid)
	} else {
		slog.Info("Stopping server (no process)", "type", serverType)
	}

	if instance.cmd.Process != nil && instance.done != nil {
		pid := instance.cmd.Process.Pid
		if err := instance.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			slog.Warn("Failed to send graceful stop signal", "type", serverType, "error", err, "pid", pid)
		} else {
			slog.Info("Sent graceful stop signal", "type", serverType, "pid", pid)
		}

		select {
		case <-instance.done:
			slog.Debug("Server process exited cleanly", "type", serverType)
		case <-time.After(gracefulStopTimeout):
			slog.Warn("Graceful shutdown timed out, forcing kill", "type", serverType, "pid", pid)
			if instance.cancel != nil {
				instance.cancel()
			}
			if err := instance.cmd.Process.Kill(); err != nil {
				slog.Error("Failed to kill server process", "type", serverType, "error", err, "pid", pid)
			} else {
				slog.Info("Server process killed", "type", serverType, "pid", pid)
			}

			select {
			case <-instance.done:
				slog.Debug("Server process exited after force kill", "type", serverType)
			case <-time.After(gracefulStopTimeout):
				slog.Warn("Timeout waiting for killed server process to exit", "type", serverType, "pid", pid)
			}
		}
	} else if instance.done != nil {
		select {
		case <-instance.done:
			slog.Debug("Server process exited cleanly", "type", serverType)
		case <-time.After(gracefulStopTimeout):
			slog.Warn("Timeout waiting for server process to exit", "type", serverType)
		}
	}

	if instance.cancel != nil {
		instance.cancel()
	}

	instance.cmd = nil
	instance.cancel = nil
	instance.done = nil
	instance.serverType = ""
	instance.modelPath = ""
	instance.port = 0

	if serverType != "" {
		if err := removeManagedServerState(serverType); err != nil {
			slog.Warn("Failed to remove managed server state", "type", serverType, "error", err)
		}
	}

	slog.Info("Server stopped", "type", serverType)
	return nil
}

func (m *Manager) Shutdown() error {
	var errs []error

	if err := m.StopChatServer(); err != nil {
		errs = append(errs, err)
	}

	if err := m.StopEmbeddingServer(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

func (m *Manager) waitForChatServer(ctx context.Context, port int) error {
	client := &http.Client{Timeout: 60 * time.Second}
	completionURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)

	slog.Info("Waiting for chat model to load and generate test response", "port", port)

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		testRequest := map[string]interface{}{
			"model":      "local-model",
			"messages":   []map[string]string{{"role": "user", "content": "hi"}},
			"max_tokens": 5,
			"stream":     false,
		}

		body, err := json.Marshal(testRequest)
		if err != nil {
			return fmt.Errorf("marshal test request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, completionURL, bytes.NewReader(body))
		if err != nil {
			slog.Debug("Failed to create request", "error", err)
			if err := waitForNextProbe(ctx, ticker); err != nil {
				return err
			}
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("Request failed, model still loading", "error", err)
			if err := waitForNextProbe(ctx, ticker); err != nil {
				return err
			}
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			slog.Debug("Failed to read response body", "error", err)
			if err := waitForNextProbe(ctx, ticker); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var response map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &response); err == nil {
				if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
					if choice, ok := choices[0].(map[string]interface{}); ok {
						if message, ok := choice["message"].(map[string]interface{}); ok {
							if content, ok := message["content"].(string); ok && content != "" {
								slog.Info("Chat model loaded and ready", "port", port)
								return nil
							}
						}
					}
				}
			}
			slog.Debug("Got 200 but no content yet, model still loading", "port", port)
		} else {
			slog.Debug("Model still loading", "port", port, "status", resp.StatusCode)
		}

		if err := waitForNextProbe(ctx, ticker); err != nil {
			return err
		}
	}
}

func (m *Manager) waitForEmbeddingServer(ctx context.Context, port int) error {
	client := &http.Client{Timeout: 10 * time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)

	slog.Info("Waiting for embedding model to load", "port", port)

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("create health check request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("Health check failed, model still loading", "error", err)
			if err := waitForNextProbe(ctx, ticker); err != nil {
				return err
			}
			continue
		}

		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			slog.Info("Embedding model loaded and ready", "port", port)
			return nil
		}

		slog.Debug("Model still loading", "port", port, "status", resp.StatusCode)
		if err := waitForNextProbe(ctx, ticker); err != nil {
			return err
		}
	}
}

func waitForNextProbe(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
		return nil
	}
}

func (m *Manager) IsChatServerRunning() bool {
	m.chat.mu.Lock()
	defer m.chat.mu.Unlock()
	return m.chat.cmd != nil
}

func (m *Manager) IsEmbeddingServerRunning() bool {
	m.embedding.mu.Lock()
	defer m.embedding.mu.Unlock()
	return m.embedding.cmd != nil
}
