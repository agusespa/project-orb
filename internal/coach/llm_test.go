package coach

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientCompleteUsesConfiguredEndpoint(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.Model != "test-model" {
			t.Fatalf("expected model %q, got %q", "test-model", req.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(streamChunk{
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: "ready"},
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(ClientConfig{
		CompletionsURL: server.URL + "/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	got, err := client.Complete(context.Background(), []chatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if got != "ready" {
		t.Fatalf("expected response %q, got %q", "ready", got)
	}
}

func TestClientStreamMessagesReportsDecodeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {not-json}\n\n"))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(ClientConfig{
		CompletionsURL: server.URL,
		Model:          "test-model",
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	tokenCh, errCh, err := client.StreamMessages(context.Background(), []chatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("StreamMessages() error = %v", err)
	}

	select {
	case _, ok := <-tokenCh:
		if ok {
			t.Fatal("expected no tokens from malformed stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for token channel to close")
	}

	select {
	case gotErr, ok := <-errCh:
		if !ok {
			t.Fatal("expected decode error, got closed channel")
		}
		if gotErr == nil {
			t.Fatal("expected non-nil decode error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream error")
	}
}
