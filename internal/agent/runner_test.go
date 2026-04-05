package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// mockRoundTripper implements http.RoundTripper for testing
type mockRoundTripper struct {
	completeDelay time.Duration
	streamTokens  []string
	streamDelay   time.Duration
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if this is a streaming request
	body, _ := io.ReadAll(req.Body)
	var reqData map[string]interface{}
	_ = json.Unmarshal(body, &reqData)

	isStream := reqData["stream"] == true

	if isStream {
		// Return streaming response
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			for _, token := range m.streamTokens {
				if m.streamDelay > 0 {
					time.Sleep(m.streamDelay)
				}
				data := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"delta": map[string]string{"content": token}},
					},
				}
				jsonData, _ := json.Marshal(data)
				_, _ = pw.Write([]byte("data: "))
				_, _ = pw.Write(jsonData)
				_, _ = pw.Write([]byte("\n\n"))
			}
			_, _ = pw.Write([]byte("data: [DONE]\n\n"))
		}()

		return &http.Response{
			StatusCode: 200,
			Body:       pr,
			Header:     make(http.Header),
		}, nil
	}

	// Non-streaming response
	if m.completeDelay > 0 {
		time.Sleep(m.completeDelay)
	}

	response := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": "mock analysis"}},
		},
	}
	jsonData, _ := json.Marshal(response)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
		Header:     make(http.Header),
	}, nil
}

func createMockClient(rt *mockRoundTripper) *Client {
	config := ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: rt},
	}
	client, _ := NewClient(config)
	return client
}

func TestRunnerCancellationDuringAnalysis(t *testing.T) {
	mockRT := &mockRoundTripper{
		completeDelay: 100 * time.Millisecond,
		streamTokens:  []string{"hello"},
	}

	client := createMockClient(mockRT)
	service, err := NewService(client, DefaultMode())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	runner := Runner{Service: service}
	session := SessionContext{}

	channels := runner.Start("test prompt", session)

	// Cancel after a short delay
	time.Sleep(10 * time.Millisecond)
	channels.Cancel()

	// Wait for result
	select {
	case result := <-channels.DoneCh:
		if !result.Canceled {
			t.Error("expected canceled=true when canceling during analysis")
		}
	case err := <-channels.ErrCh:
		// Context cancellation might surface as an error too
		if !errors.Is(err, context.Canceled) {
			t.Logf("got error (acceptable): %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for cancellation")
	}
}

func TestRunnerCancellationDuringStreaming(t *testing.T) {
	mockRT := &mockRoundTripper{
		streamTokens: []string{"hello", "world", "this", "is", "a", "test"},
		streamDelay:  50 * time.Millisecond,
	}

	client := createMockClient(mockRT)
	service, err := NewService(client, DefaultMode())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	runner := Runner{Service: service}
	session := SessionContext{}

	channels := runner.Start("test prompt", session)

	// Read a few tokens
	tokensReceived := 0
	for tokensReceived < 2 {
		select {
		case _, ok := <-channels.TokenCh:
			if ok {
				tokensReceived++
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for tokens")
		}
	}

	// Cancel mid-stream
	channels.Cancel()

	// Should receive canceled result
	select {
	case result := <-channels.DoneCh:
		if !result.Canceled {
			t.Error("expected canceled=true when canceling during streaming")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for cancellation")
	}
}

func TestRunnerCompletesSuccessfully(t *testing.T) {
	mockRT := &mockRoundTripper{
		streamTokens: []string{"hello", "world"},
	}

	client := createMockClient(mockRT)
	service, err := NewService(client, DefaultMode())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	runner := Runner{Service: service}
	session := SessionContext{}

	channels := runner.Start("test prompt", session)

	// Collect all tokens
	var tokens []string
	for token := range channels.TokenCh {
		tokens = append(tokens, token)
	}

	// Should receive non-canceled result
	select {
	case result := <-channels.DoneCh:
		if result.Canceled {
			t.Error("expected canceled=false for successful completion")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for completion")
	}

	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestRunnerHandlesNilService(t *testing.T) {
	runner := Runner{Service: nil}
	session := SessionContext{}

	channels := runner.Start("test prompt", session)

	// Should receive ErrRunnerNotConfigured
	select {
	case err := <-channels.ErrCh:
		if !errors.Is(err, ErrRunnerNotConfigured) {
			t.Errorf("expected ErrRunnerNotConfigured, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for error")
	}
}

func TestRunnerChannelsCloseAfterCompletion(t *testing.T) {
	mockRT := &mockRoundTripper{
		streamTokens: []string{"test"},
	}

	client := createMockClient(mockRT)
	service, err := NewService(client, DefaultMode())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	runner := Runner{Service: service}
	session := SessionContext{}

	channels := runner.Start("test prompt", session)

	// Drain token channel
	for range channels.TokenCh {
	}

	// Wait for completion
	<-channels.DoneCh

	// All channels should be closed
	_, tokenOk := <-channels.TokenCh
	_, errOk := <-channels.ErrCh
	_, doneOk := <-channels.DoneCh

	if tokenOk {
		t.Error("token channel should be closed")
	}
	if errOk {
		t.Error("error channel should be closed")
	}
	if doneOk {
		t.Error("done channel should be closed")
	}
}

func TestRunnerChannelsCloseAfterCancellation(t *testing.T) {
	mockRT := &mockRoundTripper{
		streamTokens: []string{"hello", "world"},
		streamDelay:  100 * time.Millisecond,
	}

	client := createMockClient(mockRT)
	service, err := NewService(client, DefaultMode())
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	runner := Runner{Service: service}
	session := SessionContext{}

	channels := runner.Start("test prompt", session)

	// Cancel immediately
	channels.Cancel()

	// Wait for done
	<-channels.DoneCh

	// All channels should eventually close
	timeout := time.After(1 * time.Second)
	for {
		select {
		case _, ok := <-channels.TokenCh:
			if !ok {
				return // Success - channel closed
			}
		case <-timeout:
			t.Fatal("token channel did not close after cancellation")
		}
	}
}
