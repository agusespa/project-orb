package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	healthCheckInterval = 500 * time.Millisecond
	serverStartTimeout  = 5 * time.Minute
	processKillWaitTime = 100 * time.Millisecond
)

func killExistingServerOnPort(port int) {
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		pidStr := strings.TrimSpace(string(output))
		if pidStr != "" {
			pid, err := strconv.Atoi(pidStr)
			if err == nil {
				slog.Info("Found existing server on port, killing it", "port", port, "pid", pid)
				killCmd := exec.Command("kill", "-9", fmt.Sprintf("%d", pid))
				if err := killCmd.Run(); err != nil {
					slog.Warn("Failed to kill process by port", "pid", pid, "error", err)
				} else {
					slog.Info("Killed existing server by port", "pid", pid)
					time.Sleep(processKillWaitTime)
					return
				}
			}
		}
	}

	psCmd := exec.Command("pgrep", "-f", fmt.Sprintf("llama-server.*--port %d", port))
	psOutput, err := psCmd.Output()
	if err == nil && len(psOutput) > 0 {
		pids := strings.Split(strings.TrimSpace(string(psOutput)), "\n")
		for _, pidStr := range pids {
			if pidStr == "" {
				continue
			}
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			slog.Info("Found existing llama-server process, killing it", "port", port, "pid", pid)
			killCmd := exec.Command("kill", "-9", fmt.Sprintf("%d", pid))
			if err := killCmd.Run(); err != nil {
				slog.Warn("Failed to kill llama-server process", "pid", pid, "error", err)
			} else {
				slog.Info("Killed existing llama-server", "pid", pid)
			}
		}
		time.Sleep(processKillWaitTime)
	}
}

type serverInstance struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.Mutex
	done   chan struct{}
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
		func(port int) error { return m.waitForChatServer(port) },
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
		func(port int) error { return m.waitForEmbeddingServer(port) },
	)
}

func (m *Manager) startServer(
	ctx context.Context,
	instance *serverInstance,
	serverType string,
	port int,
	modelPath string,
	args []string,
	waitFunc func(int) error,
) error {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.cmd != nil {
		return fmt.Errorf("%s server already running", serverType)
	}

	killExistingServerOnPort(port)

	slog.Info("Starting server", "type", serverType, "model", modelPath, "port", port)

	cmdCtx, cancel := context.WithCancel(ctx)
	instance.cancel = cancel
	instance.done = make(chan struct{})

	cmd := exec.CommandContext(cmdCtx, "llama-server", args...)

	slog.Info("Executing command", "cmd", cmd.String())

	if err := cmd.Start(); err != nil {
		cancel()
		close(instance.done)
		return fmt.Errorf("start %s server: %w", serverType, err)
	}

	instance.cmd = cmd

	waitCtx, waitCancel := context.WithTimeout(ctx, serverStartTimeout)
	defer waitCancel()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- waitFunc(port)
	}()

	select {
	case err := <-waitDone:
		if err != nil {
			_ = m.stopServerLocked(instance, serverType)
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

	// Cancel the context first to signal the process to stop
	if instance.cancel != nil {
		instance.cancel()
	}

	// Kill the process
	if instance.cmd.Process != nil {
		pid := instance.cmd.Process.Pid
		if err := instance.cmd.Process.Kill(); err != nil {
			slog.Error("Failed to kill server process", "type", serverType, "error", err, "pid", pid)
		} else {
			slog.Info("Server process killed", "type", serverType, "pid", pid)
		}
	}

	// Wait for the process to exit with a timeout
	if instance.done != nil {
		select {
		case <-instance.done:
			slog.Debug("Server process exited cleanly", "type", serverType)
		case <-time.After(2 * time.Second):
			slog.Warn("Timeout waiting for server process to exit", "type", serverType)
		}
	}

	instance.cmd = nil
	instance.cancel = nil
	instance.done = nil

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

func (m *Manager) waitForChatServer(port int) error {
	// This function is called from a goroutine in startServer, and the parent
	// context cancellation is handled by the select statement there.
	// We don't need to check context here since the goroutine will be abandoned
	// when the select returns on context cancellation.

	client := &http.Client{Timeout: 60 * time.Second}
	completionURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)

	slog.Info("Waiting for chat model to load and generate test response", "port", port)

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
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

		req, err := http.NewRequest(http.MethodPost, completionURL, bytes.NewReader(body))
		if err != nil {
			slog.Debug("Failed to create request", "error", err)
			<-ticker.C
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("Request failed, model still loading", "error", err)
			<-ticker.C
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			slog.Debug("Failed to read response body", "error", err)
			<-ticker.C
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

		<-ticker.C
	}
}

func (m *Manager) waitForEmbeddingServer(port int) error {
	client := &http.Client{Timeout: 10 * time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)

	slog.Info("Waiting for embedding model to load", "port", port)

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		resp, err := client.Get(healthURL)
		if err != nil {
			slog.Debug("Health check failed, model still loading", "error", err)
			<-ticker.C
			continue
		}

		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			slog.Info("Embedding model loaded and ready", "port", port)
			return nil
		}

		slog.Debug("Model still loading", "port", port, "status", resp.StatusCode)
		<-ticker.C
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
