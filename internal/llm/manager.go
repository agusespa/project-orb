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
)

// killExistingServerOnPort kills any llama-server process listening on the given port
func killExistingServerOnPort(port int) error {
	// First try to find by port using lsof
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
					time.Sleep(100 * time.Millisecond)
					return nil
				}
			}
		}
	}

	// Also check for any llama-server processes with this port in args
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
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

type Manager struct {
	config *Config

	chatCmd    *exec.Cmd
	chatCancel context.CancelFunc
	chatMu     sync.Mutex

	embeddingCmd    *exec.Cmd
	embeddingCancel context.CancelFunc
	embeddingMu     sync.Mutex
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

// StartChatServer starts the llama.cpp server for chat
func (m *Manager) StartChatServer(ctx context.Context) error {
	m.chatMu.Lock()
	defer m.chatMu.Unlock()

	if m.chatCmd != nil {
		return fmt.Errorf("chat server already running")
	}

	// Kill any existing llama-server processes on this port
	if err := killExistingServerOnPort(m.config.LlamaCpp.ChatPort); err != nil {
		slog.Warn("Failed to kill existing server on port", "port", m.config.LlamaCpp.ChatPort, "error", err)
	}

	slog.Info("Starting chat server", "model", m.config.ChatModelPath(), "port", m.config.LlamaCpp.ChatPort)

	cmdCtx, cancel := context.WithCancel(ctx)
	m.chatCancel = cancel

	cmd := exec.CommandContext(
		cmdCtx,
		"llama-server",
		"-m", m.config.ChatModelPath(),
		"--alias", "local-model",
		"--port", fmt.Sprintf("%d", m.config.LlamaCpp.ChatPort),
		"--host", "127.0.0.1",
		"-c", "32768", // context size
		"-n", "8192", // max tokens to predict
		"-ngl", "99", // GPU layers
		"-b", "2048", // batch size
		"-ub", "1024", // ubatch size
		"--temp", "0.7",
		"--top-p", "0.95",
		"--top-k", "20",
		"--min-p", "0.00",
		"-np", "1",
		"-fa", "auto",
		"--chat-template-kwargs", `{"enable_thinking":false}`,
	)

	slog.Info("Executing command", "cmd", cmd.String())

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start chat server: %w", err)
	}

	m.chatCmd = cmd

	// Wait for server to be ready
	_, err := m.waitForServer(m.config.LlamaCpp.ChatPort)
	if err != nil {
		_ = m.stopChatServer()
		return fmt.Errorf("chat server failed to start: %w", err)
	}

	slog.Info("Chat server ready", "port", m.config.LlamaCpp.ChatPort)

	// Monitor process in background
	go func() {
		if err := cmd.Wait(); err != nil && cmdCtx.Err() == nil {
			slog.Error("Chat server exited unexpectedly", "error", err)
		}
	}()

	return nil
}

// StartEmbeddingServer starts the llama.cpp server for embeddings
func (m *Manager) StartEmbeddingServer(ctx context.Context) error {
	m.embeddingMu.Lock()
	defer m.embeddingMu.Unlock()

	if m.embeddingCmd != nil {
		return fmt.Errorf("embedding server already running")
	}

	// Kill any existing llama-server processes on this port
	if err := killExistingServerOnPort(m.config.LlamaCpp.EmbeddingPort); err != nil {
		slog.Warn("Failed to kill existing server on port", "port", m.config.LlamaCpp.EmbeddingPort, "error", err)
	}

	slog.Info("Starting embedding server", "model", m.config.EmbeddingModelPath(), "port", m.config.LlamaCpp.EmbeddingPort)

	cmdCtx, cancel := context.WithCancel(ctx)
	m.embeddingCancel = cancel

	cmd := exec.CommandContext(
		cmdCtx,
		"llama-server",
		"-m", m.config.EmbeddingModelPath(),
		"--port", fmt.Sprintf("%d", m.config.LlamaCpp.EmbeddingPort),
		"--host", "127.0.0.1",
		"--embedding",
		"-ngl", "99",
	)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start embedding server: %w", err)
	}

	m.embeddingCmd = cmd

	// Wait for server to be ready
	_, waitErr := m.waitForServer(m.config.LlamaCpp.EmbeddingPort)
	if waitErr != nil {
		_ = m.stopEmbeddingServer()
		return fmt.Errorf("embedding server failed to start: %w", waitErr)
	}

	slog.Info("Embedding server ready", "port", m.config.LlamaCpp.EmbeddingPort)

	// Monitor process in background
	go func() {
		if err := cmd.Wait(); err != nil && cmdCtx.Err() == nil {
			slog.Error("Embedding server exited unexpectedly", "error", err)
		}
	}()

	return nil
}

// StopChatServer stops the chat server
func (m *Manager) StopChatServer() error {
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	return m.stopChatServer()
}

func (m *Manager) stopChatServer() error {
	if m.chatCmd == nil {
		slog.Info("Chat server already stopped or not started")
		return nil
	}

	if m.chatCmd.Process != nil {
		slog.Info("Stopping chat server", "pid", m.chatCmd.Process.Pid)
	} else {
		slog.Info("Stopping chat server (no process)")
	}

	// Cancel context first
	if m.chatCancel != nil {
		m.chatCancel()
	}

	// Kill the process directly - don't wait
	if m.chatCmd.Process != nil {
		pid := m.chatCmd.Process.Pid
		if err := m.chatCmd.Process.Kill(); err != nil {
			slog.Error("Failed to kill chat server process", "error", err, "pid", pid)
		} else {
			slog.Info("Chat server process killed", "pid", pid)
		}
	}

	// Don't wait for the process - just clean up our references
	m.chatCmd = nil
	m.chatCancel = nil

	slog.Info("Chat server stopped")
	return nil
}

// StopEmbeddingServer stops the embedding server
func (m *Manager) StopEmbeddingServer() error {
	m.embeddingMu.Lock()
	defer m.embeddingMu.Unlock()
	return m.stopEmbeddingServer()
}

func (m *Manager) stopEmbeddingServer() error {
	if m.embeddingCmd == nil {
		slog.Info("Embedding server already stopped or not started")
		return nil
	}

	if m.embeddingCmd.Process != nil {
		slog.Info("Stopping embedding server", "pid", m.embeddingCmd.Process.Pid)
	} else {
		slog.Info("Stopping embedding server (no process)")
	}

	// Cancel context first
	if m.embeddingCancel != nil {
		m.embeddingCancel()
	}

	// Kill the process directly - don't wait
	if m.embeddingCmd.Process != nil {
		pid := m.embeddingCmd.Process.Pid
		if err := m.embeddingCmd.Process.Kill(); err != nil {
			slog.Error("Failed to kill embedding server process", "error", err, "pid", pid)
		} else {
			slog.Info("Embedding server process killed", "pid", pid)
		}
	}

	// Don't wait for the process - just clean up our references
	m.embeddingCmd = nil
	m.embeddingCancel = nil

	slog.Info("Embedding server stopped")
	return nil
}

// Shutdown stops all running servers
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

// waitForServer waits for the server to be ready by making a test completion request
func (m *Manager) waitForServer(port int) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second} // Long timeout for generation
	completionURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)

	slog.Info("Waiting for model to load and generate test response", "port", port)

	for {
		// Make a minimal test completion request
		testRequest := map[string]interface{}{
			"model":      "local-model",
			"messages":   []map[string]string{{"role": "user", "content": "Say hi"}},
			"max_tokens": 5,
			"stream":     false,
		}

		body, err := json.Marshal(testRequest)
		if err != nil {
			return "", fmt.Errorf("marshal test request: %w", err)
		}

		req, err := http.NewRequest(http.MethodPost, completionURL, bytes.NewReader(body))
		if err != nil {
			slog.Debug("Failed to create request", "error", err)
			time.Sleep(healthCheckInterval)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			slog.Debug("Request failed, model still loading", "error", err)
			time.Sleep(healthCheckInterval)
			continue
		}

		// Read and close body
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		slog.Debug("Health check response", "status", resp.StatusCode, "body_length", len(bodyBytes))

		// 200 OK means request completed
		if resp.StatusCode == http.StatusOK {
			// Parse response to check if we got actual content
			var response map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &response); err == nil {
				if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
					if choice, ok := choices[0].(map[string]interface{}); ok {
						if message, ok := choice["message"].(map[string]interface{}); ok {
							if content, ok := message["content"].(string); ok && content != "" {
								slog.Info("Model loaded and ready - generated content", "port", port, "content", content)
								return string(bodyBytes), nil
							}
						}
					}
				}
			}
			// Got 200 but no content, model still loading
			slog.Debug("Got 200 but no content yet, model still loading", "port", port)
			time.Sleep(healthCheckInterval)
			continue
		}

		// 503 or other errors mean model still loading
		slog.Debug("Model still loading", "port", port, "status", resp.StatusCode)
		time.Sleep(healthCheckInterval)
	}
}

// IsChatServerRunning returns true if the chat server is running
func (m *Manager) IsChatServerRunning() bool {
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	return m.chatCmd != nil
}

// IsEmbeddingServerRunning returns true if the embedding server is running
func (m *Manager) IsEmbeddingServerRunning() bool {
	m.embeddingMu.Lock()
	defer m.embeddingMu.Unlock()
	return m.embeddingCmd != nil
}
