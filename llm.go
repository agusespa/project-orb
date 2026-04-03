package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	personaFile      = "persona.md"
	instructionsFile = "instructions.md"
	completionsURL   = "http://localhost:8080/v1/chat/completions"
	modelName        = "local-model"
)

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
	} `json:"choices"`
}

func LoadSystemMessage() (string, error) {
	persona, err := os.ReadFile(personaFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("missing %s in the working directory", personaFile)
		}
		return "", fmt.Errorf("read %s: %w", personaFile, err)
	}

	instructions, err := os.ReadFile(instructionsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("missing %s in the working directory", instructionsFile)
		}
		return "", fmt.Errorf("read %s: %w", instructionsFile, err)
	}

	return string(persona) + "\n\n---\n\n" + string(instructions), nil
}

func StreamCompletion(ctx context.Context, userInput string, tokenCh chan<- string) error {
	systemMessage, err := LoadSystemMessage()
	if err != nil {
		return err
	}

	payload := chatRequest{
		Model:  modelName,
		Stream: true,
		Messages: []chatMessage{
			{Role: "system", Content: systemMessage},
			{Role: "user", Content: userInput},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, completionsURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("llm server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decode stream chunk: %w", err)
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
			return ctx.Err()
		case tokenCh <- token:
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}

	return nil
}
