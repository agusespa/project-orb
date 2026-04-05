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
)

const processKillWaitTime = 100 * time.Millisecond

// killExistingServerOnPort kills any llama-server process listening on the given port
func killExistingServerOnPort(port int) {
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
					time.Sleep(processKillWaitTime)
					return
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
		time.Sleep(processKillWaitTime)
	}
}

type Manager struct {
	config *Config

	chatCmd    *exec.Cmd
	chatCancel context.CancelFunc
	chatMu     sync.Mutex
	chatDone   chan struct{}

	embeddingCmd    *exec.Cmd
	embeddingCancel context.CancelFunc
	embeddingMu     sync.Mutex
	embeddingDone   chan struct{}
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

	killExistingServerOnPort(m.config.LlamaCpp.ChatPort)

	slog.Info("Starting chat server", "model", m.config.ChatModelPath(), "port", m.config.LlamaCpp.ChatPort)

	cmdCtx, cancel := context.WithCancel(ctx)
	m.chatCancel = cancel
	m.chatDone = make(chan struct{})

	cmd := exec.CommandContext(
		cmdCtx,
		"llama-server",
		"-m", m.config.ChatModelPath(),
		"--alias", "local-model",
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
	)

	slog.Info("Executing command", "cmd", cmd.String())

	if err := cmd.Start(); err != nil {
		cancel()
		close(m.chatDone)
		return fmt.Errorf("start chat server: %w", err)
	}

	m.chatCmd = cmd

	// Wait for server to be ready
	if err := m.waitForChatServer(m.config.LlamaCpp.ChatPort); err != nil {
		_ = m.stopChatServer()
		return fmt.Errorf("chat server failed to start: %w", err)
	}

	slog.Info("Chat server ready", "port", m.config.LlamaCpp.ChatPort)

	// Monitor process in background
	go func() {
		defer close(m.chatDone)
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

	killExistingServerOnPort(m.config.LlamaCpp.EmbeddingPort)

	slog.Info("Starting embedding server", "model", m.config.EmbeddingModelPath(), "port", m.config.LlamaCpp.EmbeddingPort)

	cmdCtx, cancel := context.WithCancel(ctx)
	m.embeddingCancel = cancel
	m.embeddingDone = make(chan struct{})

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
		close(m.embeddingDone)
		return fmt.Errorf("start embedding server: %w", err)
	}

	m.embeddingCmd = cmd

	// Wait for server to be ready
	if err := m.waitForEmbeddingServer(m.config.LlamaCpp.EmbeddingPort); err != nil {
		_ = m.stopEmbeddingServer()
		return fmt.Errorf("embedding server failed to start: %w", err)
	}

	slog.Info("Embedding server ready", "port", m.config.LlamaCpp.EmbeddingPort)

	// Monitor process in background
	go func() {
		defer close(m.embeddingDone)
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

	// Kill the process
	if m.chatCmd.Process != nil {
		pid := m.chatCmd.Process.Pid
		if err := m.chatCmd.Process.Kill(); err != nil {
			slog.Error("Failed to kill chat server process", "error", err, "pid", pid)
		} else {
			slog.Info("Chat server process killed", "pid", pid)
		}
	}

	// Wait for goroutine to finish
	if m.chatDone != nil {
		<-m.chatDone
	}

	m.chatCmd = nil
	m.chatCancel = nil
	m.chatDone = nil

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

	// Kill the process
	if m.embeddingCmd.Process != nil {
		pid := m.embeddingCmd.Process.Pid
		if err := m.embeddingCmd.Process.Kill(); err != nil {
			slog.Error("Failed to kill embedding server process", "error", err, "pid", pid)
		} else {
			slog.Info("Embedding server process killed", "pid", pid)
		}
	}

	// Wait for goroutine to finish
	if m.embeddingDone != nil {
		<-m.embeddingDone
	}

	m.embeddingCmd = nil
	m.embeddingCancel = nil
	m.embeddingDone = nil

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

// waitForChatServer waits for the chat server to be ready by making a test completion request
func (m *Manager) waitForChatServer(port int) error {
	client := &http.Client{Timeout: 60 * time.Second}
	completionURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port)

	slog.Info("Waiting for chat model to load and generate test response", "port", port)

	startTime := time.Now()
	for {
		if time.Since(startTime) > serverStartTimeout {
			return fmt.Errorf("chat server failed to start within %v", serverStartTimeout)
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

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

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

		time.Sleep(healthCheckInterval)
	}
}

// waitForEmbeddingServer waits for the embedding server to be ready by checking health endpoint
func (m *Manager) waitForEmbeddingServer(port int) error {
	client := &http.Client{Timeout: 10 * time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)

	slog.Info("Waiting for embedding model to load", "port", port)

	startTime := time.Now()
	for {
		if time.Since(startTime) > serverStartTimeout {
			return fmt.Errorf("embedding server failed to start within %v", serverStartTimeout)
		}

		resp, err := client.Get(healthURL)
		if err != nil {
			slog.Debug("Health check failed, model still loading", "error", err)
			time.Sleep(healthCheckInterval)
			continue
		}

		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			slog.Info("Embedding model loaded and ready", "port", port)
			return nil
		}

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
