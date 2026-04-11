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

	"project-orb/internal/paths"
	"project-orb/internal/text"
)

const (
	defaultCompletionsURL = "http://localhost:8080/v1/chat/completions"
	defaultModelName      = "local-model"
)

//go:embed prompts/instructions.md
var embeddedInstructions string

var personaNamePattern = regexp.MustCompile(`(?m)^Your name is (.+)\.$`)

type chatRequest struct {
	Model      string        `json:"model"`
	Stream     bool          `json:"stream"`
	Messages   []chatMessage `json:"messages"`
	Tools      []chatTool    `json:"tools,omitempty"`
	ToolChoice any           `json:"tool_choice,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type completionResponse struct {
	Choices []completionChoice `json:"choices"`
}

type completionChoice struct {
	Delta   chatMessage `json:"delta"`
	Message chatMessage `json:"message"`
}

type ToolHandler struct {
	Definition chatTool
	Execute    func(context.Context, json.RawMessage) (string, error)
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

func LoadAgentName() (string, error) {
	persona, err := LoadPersona()
	if err != nil {
		return "", err
	}

	if name := ExtractAgentName(persona); name != "" {
		return name, nil
	}

	return text.DefaultAgentName, nil
}

func ExtractAgentName(persona string) string {
	match := personaNamePattern.FindStringSubmatch(persona)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func EnsurePersonaFile() (string, error) {
	personaPath, err := paths.PersonaFilePath()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(personaPath), 0o755); err != nil {
		return "", fmt.Errorf("create config dir %s: %w", filepath.Dir(personaPath), err)
	}

	if _, err := os.Stat(personaPath); err == nil {
		return personaPath, nil
	}

	defaultPersona := fmt.Sprintf("# Persona\n\nYou are %s.\n", text.DefaultPersonality)
	if err := os.WriteFile(personaPath, []byte(defaultPersona), 0o644); err != nil {
		return "", fmt.Errorf("create default persona at %s: %w", personaPath, err)
	}

	return personaPath, nil
}

func (c *Client) Complete(ctx context.Context, messages []chatMessage) (string, error) {
	message, err := c.completeMessage(ctx, chatRequest{
		Model:    c.model,
		Stream:   false,
		Messages: messages,
	})
	if err != nil {
		return "", err
	}

	return message.Content, nil
}

func (c *Client) CompleteWithTools(ctx context.Context, messages []chatMessage, handlers []ToolHandler) (chatMessage, error) {
	if len(handlers) == 0 {
		return c.completeMessage(ctx, chatRequest{
			Model:    c.model,
			Stream:   false,
			Messages: messages,
		})
	}

	tools := make([]chatTool, 0, len(handlers))
	toolMap := make(map[string]ToolHandler, len(handlers))
	for _, handler := range handlers {
		tools = append(tools, handler.Definition)
		toolMap[handler.Definition.Function.Name] = handler
	}

	conversation := append([]chatMessage(nil), messages...)
	for i := 0; i < 6; i++ {
		message, err := c.completeMessage(ctx, chatRequest{
			Model:      c.model,
			Stream:     false,
			Messages:   conversation,
			Tools:      tools,
			ToolChoice: "auto",
		})
		if err != nil {
			return chatMessage{}, err
		}

		if len(message.ToolCalls) == 0 {
			return message, nil
		}

		conversation = append(conversation, message)
		for _, call := range message.ToolCalls {
			handler, ok := toolMap[call.Function.Name]
			if !ok {
				conversation = append(conversation, chatMessage{
					Role:       "tool",
					Name:       call.Function.Name,
					ToolCallID: call.ID,
					Content:    toolErrorJSON(fmt.Sprintf("unknown tool %q", call.Function.Name)),
				})
				continue
			}

			result, err := handler.Execute(ctx, json.RawMessage(call.Function.Arguments))
			if err != nil {
				result = toolErrorJSON(err.Error())
			}

			conversation = append(conversation, chatMessage{
				Role:       "tool",
				Name:       call.Function.Name,
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}

	return chatMessage{}, fmt.Errorf("tool calling exceeded maximum iterations")
}

func toolErrorJSON(message string) string {
	data, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": message,
	})
	return string(data)
}

func (c *Client) completeMessage(ctx context.Context, payload chatRequest) (chatMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return chatMessage{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.completionsURL, bytes.NewReader(body))
	if err != nil {
		return chatMessage{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return chatMessage{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return chatMessage{}, fmt.Errorf("llm server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var response completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return chatMessage{}, fmt.Errorf("decode completion response: %w", err)
	}

	if len(response.Choices) == 0 {
		return chatMessage{}, fmt.Errorf("completion response contained no choices")
	}

	return response.Choices[0].Message, nil
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

			var chunk completionResponse
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
