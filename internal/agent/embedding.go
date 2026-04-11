package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"project-orb/internal/llm"
)

const (
	defaultEmbeddingsURL = "http://localhost:8081/v1/embeddings"
)

type embedder interface {
	Embed(context.Context, string) ([]float64, error)
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

type EmbeddingClient struct {
	url        string
	model      string
	httpClient *http.Client
}

func NewEmbeddingClient() *EmbeddingClient {
	config, err := llm.LoadConfig()
	if err != nil {
		return &EmbeddingClient{
			url:        defaultEmbeddingsURL,
			model:      defaultModelName,
			httpClient: &http.Client{},
		}
	}

	return &EmbeddingClient{
		url:        fmt.Sprintf("http://127.0.0.1:%d/v1/embeddings", config.LlamaCpp.EmbeddingPort),
		model:      defaultModelName,
		httpClient: &http.Client{},
	}
}

func (c *EmbeddingClient) Embed(ctx context.Context, input string) ([]float64, error) {
	if c == nil {
		return nil, fmt.Errorf("embedding client is nil")
	}

	payload := embeddingRequest{
		Model: c.model,
		Input: strings.TrimSpace(input),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embedding server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var decoded embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(decoded.Data) == 0 || len(decoded.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding response contained no vectors")
	}

	return decoded.Data[0].Embedding, nil
}
