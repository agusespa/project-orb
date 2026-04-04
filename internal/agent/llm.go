package agent

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	appName               = "project-orb"
	personaFile           = "persona.md"
	defaultCompletionsURL = "http://localhost:8080/v1/chat/completions"
	defaultModelName      = "local-model"
)

//go:embed prompts/persona.md
var defaultPersona string

//go:embed prompts/instructions.md
var embeddedInstructions string

var personaNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^\s*(?:[-*]\s*)?name\s*:\s*([A-Za-z][A-Za-z0-9 _-]{0,40})\s*$`),
	regexp.MustCompile(`(?im)^\s*(?:[-*]\s*)?your name is\s+([A-Za-z][A-Za-z0-9 _-]{0,40})[.!]?\s*$`),
	regexp.MustCompile(`(?im)^\s*(?:[-*]\s*)?you are\s+([A-Za-z][A-Za-z0-9 _-]{0,40})[.!]?\s*$`),
}

type chatRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type ClientConfig struct {
	CompletionsURL string
	Model          string
	HTTPClient     *http.Client
}

type Client struct {
	completionsURL string
	model          string
	httpClient     *http.Client
}

func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		CompletionsURL: defaultCompletionsURL,
		Model:          defaultModelName,
		HTTPClient:     &http.Client{},
	}
}

func NewClient(config ClientConfig) (*Client, error) {
	if strings.TrimSpace(config.CompletionsURL) == "" {
		return nil, errors.New("completions URL cannot be empty")
	}

	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("model cannot be empty")
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Client{
		completionsURL: config.CompletionsURL,
		model:          config.Model,
		httpClient:     httpClient,
	}, nil
}

func LoadSystemMessage() (string, error) {
	persona, err := LoadPersona()
	if err != nil {
		return "", err
	}

	return persona + "\n\n---\n\n" + embeddedInstructions, nil
}

func LoadPersona() (string, error) {
	personaPath, err := EnsurePersonaFile()
	if err != nil {
		return "", err
	}

	persona, err := os.ReadFile(personaPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", personaPath, err)
	}

	return string(persona), nil
}

func LoadCoachName() (string, error) {
	persona, err := LoadPersona()
	if err != nil {
		return "", err
	}

	if name := ExtractCoachName(persona); name != "" {
		return name, nil
	}

	return "Coach", nil
}

func ExtractCoachName(persona string) string {
	trimmed := strings.TrimSpace(persona)
	for _, pattern := range personaNamePatterns {
		match := pattern.FindStringSubmatch(trimmed)
		if len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}

	return ""
}

func EnsurePersonaFile() (string, error) {
	appConfigDir, err := resolveAppConfigDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir %s: %w", appConfigDir, err)
	}

	personaPath := filepath.Join(appConfigDir, personaFile)
	if _, err := os.Stat(personaPath); err == nil {
		return personaPath, nil
	}

	if err := os.WriteFile(personaPath, []byte(defaultPersona), 0o644); err != nil {
		return "", fmt.Errorf("create default persona at %s: %w", personaPath, err)
	}

	return personaPath, nil
}

func resolveAppConfigDir() (string, error) {
	if xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, appName), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}

	return filepath.Join(homeDir, ".config", appName), nil
}

func (c *Client) Complete(ctx context.Context, messages []chatMessage) (string, error) {
	payload := chatRequest{
		Model:    c.model,
		Stream:   false,
		Messages: messages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.completionsURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("llm server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var chunk streamChunk
	if err := json.NewDecoder(resp.Body).Decode(&chunk); err != nil {
		return "", fmt.Errorf("decode completion response: %w", err)
	}

	if len(chunk.Choices) == 0 {
		return "", fmt.Errorf("completion response contained no choices")
	}

	return chunk.Choices[0].Message.Content, nil
}

func (c *Client) StreamMessages(ctx context.Context, messages []chatMessage) (<-chan string, <-chan error, error) {
	payload := chatRequest{
		Model:    c.model,
		Stream:   true,
		Messages: messages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.completionsURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, nil, fmt.Errorf("llm server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			// Check for cancellation before processing
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				errCh <- fmt.Errorf("decode stream chunk: %w", err)
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			token := chunk.Choices[0].Delta.Content
			if token == "" {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case tokenCh <- token:
			}
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("read stream: %w", err)
		}
	}()

	return tokenCh, errCh, nil
}

