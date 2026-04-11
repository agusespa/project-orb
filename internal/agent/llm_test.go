package agent

import (
	"context"
	"encoding/json"
	"errors"
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
		if err := json.NewEncoder(w).Encode(completionResponse{
			Choices: []completionChoice{
				{
					Message: chatMessage{Content: "ready"},
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

func TestClientCompleteWithToolsExecutesToolCalls(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		switch requests {
		case 1:
			if len(req.Tools) != 1 {
				t.Fatalf("expected 1 tool in request, got %d", len(req.Tools))
			}
			_ = json.NewEncoder(w).Encode(completionResponse{
				Choices: []completionChoice{
					{
						Message: chatMessage{
							Role: "assistant",
							ToolCalls: []chatToolCall{
								{
									ID:   "call-1",
									Type: "function",
									Function: chatToolCallFunction{
										Name:      "lookup_memory",
										Arguments: `{"query":"fear of shipping"}`,
									},
								},
							},
						},
					},
				},
			})
		case 2:
			if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role != "tool" {
				t.Fatalf("expected tool result message in second request, got %#v", req.Messages)
			}
			_ = json.NewEncoder(w).Encode(completionResponse{
				Choices: []completionChoice{
					{
						Message: chatMessage{Role: "assistant", Content: "final answer"},
					},
				},
			})
		default:
			t.Fatalf("unexpected extra request %d", requests)
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

	called := 0
	message, err := client.CompleteWithTools(context.Background(), []chatMessage{{Role: "user", Content: "hello"}}, []ToolHandler{
		{
			Definition: chatTool{
				Type: "function",
				Function: chatToolFunction{
					Name: "lookup_memory",
				},
			},
			Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
				called++
				return `{"ok":true}`, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("CompleteWithTools() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("expected tool to be executed once, got %d", called)
	}
	if message.Content != "final answer" {
		t.Fatalf("expected final answer, got %q", message.Content)
	}
}

// TestStreamMessagesCancellation verifies that context cancellation stops streaming
func TestStreamMessagesCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send a few tokens
		for i := 0; i < 3; i++ {
			chunk := completionResponse{
				Choices: []completionChoice{
					{
						Delta: chatMessage{Content: "token"},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Keep connection open to simulate long stream
		time.Sleep(5 * time.Second)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
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

	ctx, cancel := context.WithCancel(context.Background())
	tokenCh, errCh, err := client.StreamMessages(ctx, []chatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("StreamMessages() error = %v", err)
	}

	// Receive a few tokens
	tokensReceived := 0
	for i := 0; i < 3; i++ {
		select {
		case _, ok := <-tokenCh:
			if !ok {
				t.Fatal("token channel closed unexpectedly")
			}
			tokensReceived++
		case err := <-errCh:
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timed out waiting for token")
		}
	}

	// Cancel the context
	cancel()

	// Verify channels close promptly
	select {
	case _, ok := <-tokenCh:
		if ok {
			t.Fatal("expected token channel to close after cancellation")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("token channel did not close after cancellation")
	}

	if tokensReceived != 3 {
		t.Fatalf("expected 3 tokens before cancellation, got %d", tokensReceived)
	}
}

// TestCompleteWithCanceledContext verifies Complete respects context cancellation
func TestCompleteWithCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(completionResponse{
			Choices: []completionChoice{
				{
					Message: chatMessage{Content: "response"},
				},
			},
		})
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = client.Complete(ctx, []chatMessage{{Role: "user", Content: "hello"}})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}
}

// TestStreamMessagesWithTimeout verifies timeout behavior
func TestStreamMessagesWithTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Send one token then hang
		chunk := completionResponse{
			Choices: []completionChoice{
				{
					Delta: chatMessage{Content: "token"},
				},
			},
		}
		data, _ := json.Marshal(chunk)
		_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Simulate very slow stream
		time.Sleep(5 * time.Second)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	tokenCh, errCh, err := client.StreamMessages(ctx, []chatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("StreamMessages() error = %v", err)
	}

	// Should receive at least one token
	select {
	case _, ok := <-tokenCh:
		if !ok {
			t.Fatal("expected at least one token before timeout")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for first token")
	}

	// Wait for timeout to trigger closure
	select {
	case _, ok := <-tokenCh:
		if ok {
			// Might receive more tokens, keep draining
			for range tokenCh {
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("token channel did not close after timeout")
	}

	// Error channel should also close
	select {
	case _, ok := <-errCh:
		if ok {
			// It's ok if an error is sent, drain it
			for range errCh {
			}
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("error channel did not close after timeout")
	}
}

// TestStreamMessagesContextCheckInLoop verifies context is checked during streaming
func TestStreamMessagesContextCheckInLoop(t *testing.T) {
	tokensSent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Send many tokens
		for i := 0; i < 100; i++ {
			chunk := completionResponse{
				Choices: []completionChoice{
					{
						Delta: chatMessage{Content: "x"},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			tokensSent++
			time.Sleep(10 * time.Millisecond)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
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

	ctx, cancel := context.WithCancel(context.Background())
	tokenCh, errCh, err := client.StreamMessages(ctx, []chatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("StreamMessages() error = %v", err)
	}

	// Receive a few tokens then cancel
	tokensReceived := 0
	for i := 0; i < 5; i++ {
		select {
		case _, ok := <-tokenCh:
			if !ok {
				t.Fatal("token channel closed too early")
			}
			tokensReceived++
		case err := <-errCh:
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("timed out waiting for token")
		}
	}

	cancel()

	// Verify we don't receive all 100 tokens
	for {
		select {
		case _, ok := <-tokenCh:
			if !ok {
				// Channel closed, good
				if tokensReceived >= 100 {
					t.Fatalf("received all %d tokens despite cancellation", tokensReceived)
				}
				return
			}
			tokensReceived++
		case <-time.After(500 * time.Millisecond):
			t.Fatal("token channel did not close after cancellation")
		}
	}
}
