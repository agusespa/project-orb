package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"project-orb/internal/text"
)

type stubSessionStore struct {
	memories        []MemorySnippet
	transcript      string
	transcriptCalls int
}

func (s *stubSessionStore) LoadLatestSummary(mode ModeID) (SessionContext, bool, error) {
	return NewSessionContext(), false, nil
}

func (s *stubSessionStore) SaveSession(ctx context.Context, mode ModeID, session SessionContext) error {
	return nil
}

func (s *stubSessionStore) SearchRelevantSummaries(mode ModeID, query string, excludeSummary string, limit int) ([]MemorySnippet, error) {
	if len(s.memories) > limit {
		return s.memories[:limit], nil
	}
	return s.memories, nil
}

func (s *stubSessionStore) LoadTranscriptExcerpt(mode ModeID, sessionID string, query string, maxTurns int) (string, error) {
	s.transcriptCalls++
	return s.transcript, nil
}

func TestBuildConversationMessagesIncludesMemories(t *testing.T) {
	messages := buildConversationMessages("system", SessionContext{
		Summary: "Current summary",
		Recent: []Turn{
			{User: "Current user", Assistant: "Current assistant"},
		},
	}, []MemorySnippet{
		{
			SessionID: "2026-04-05-120000",
			Summary:   "## Overview\nPast memory",
			Excerpt:   "User: I was afraid of shipping.\nAssistant: We named perfectionism.",
			Score:     2,
		},
	})

	if len(messages) < 4 {
		t.Fatalf("expected memory message to be included, got %d messages", len(messages))
	}
	if !strings.Contains(messages[1].Content, "Relevant past session summaries:") {
		t.Fatalf("expected memory context in second message, got %q", messages[1].Content)
	}
	if !strings.Contains(messages[1].Content, "Supporting transcript excerpt:") {
		t.Fatalf("expected transcript excerpt in memory context, got %q", messages[1].Content)
	}
}

func TestAnalysisToolsOnlyForAnalystMode(t *testing.T) {
	service := &Service{
		mode:  DefaultMode(),
		store: &stubSessionStore{memories: []MemorySnippet{{SessionID: "1", Summary: "Past memory", Score: 1}}},
	}

	if got := service.analysisTools("prompt", SessionContext{}); len(got) != 0 {
		t.Fatalf("expected no tools for non-analyst mode, got %d", len(got))
	}
}

func TestSearchMemoriesToolUsesStoreForAnalystMode(t *testing.T) {
	store := &stubSessionStore{
		memories: []MemorySnippet{{SessionID: "1", Summary: "Past memory", Score: 8}},
	}
	service := &Service{
		mode: Mode{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
		store: store,
	}

	tool := service.searchMemoriesTool("prompt", SessionContext{Summary: "Current summary"})
	got, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got, "\"session_id\":\"1\"") {
		t.Fatalf("expected memory results in tool output, got %q", got)
	}
}

func TestLoadMemoryExcerptToolLoadsTranscriptExcerpt(t *testing.T) {
	store := &stubSessionStore{
		memories:   []MemorySnippet{{SessionID: "1", Summary: "Past memory", Score: 8}},
		transcript: "User: You said you were scared.\nAssistant: We traced it to fear of being judged.",
	}
	service := &Service{
		mode: Mode{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
		store: store,
	}

	tool := service.loadMemoryExcerptTool("What exactly was I afraid would happen last time?")
	got, err := tool.Execute(context.Background(), json.RawMessage(`{"session_id":"1"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(got) == "" || !strings.Contains(got, "fear of being judged") {
		t.Fatalf("expected transcript excerpt in tool output, got %q", got)
	}
	if store.transcriptCalls != 1 {
		t.Fatalf("expected exactly one transcript load, got %d", store.transcriptCalls)
	}
}

type startupRoundTripper struct {
	content string
}

func (s startupRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	response := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]string{
					"content": s.content,
				},
			},
		},
	}
	jsonData, _ := json.Marshal(response)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
		Header:     make(http.Header),
	}, nil
}

type startupInspectingRoundTripper struct {
	t               *testing.T
	responseContent string
}

func (s startupInspectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	payload := string(body)
	if !strings.Contains(payload, "Role mapping for this task:") {
		s.t.Fatalf("expected startup request to include role mapping guidance, payload=%q", payload)
	}
	if !strings.Contains(payload, "Do not address the user as Claudio") {
		s.t.Fatalf("expected startup request to include name-safety guidance, payload=%q", payload)
	}

	response := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]string{
					"content": s.responseContent,
				},
			},
		},
	}
	jsonData, _ := json.Marshal(response)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
		Header:     make(http.Header),
	}, nil
}

func TestStartupMessagesWithoutSummaryUseHardcodedFallback(t *testing.T) {
	service := &Service{
		mode: Mode{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
	}

	messages, err := service.StartupMessages(context.Background(), SessionContext{})
	if err != nil {
		t.Fatalf("StartupMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 startup message, got %d", len(messages))
	}
	if messages[0] != text.AnalystWelcomeMessage {
		t.Fatalf("expected welcome message, got %q", messages[0])
	}

	// Test the second message is loaded separately
	secondMessage, err := service.LoadAnalystSecondMessage(context.Background(), SessionContext{})
	if err != nil {
		t.Fatalf("LoadAnalystSecondMessage() error = %v", err)
	}
	if secondMessage != text.AnalystFreshStartMessage {
		t.Fatalf("expected fresh-start message, got %q", secondMessage)
	}
}

func TestStartupMessagesForCoachUseWelcomeMessage(t *testing.T) {
	service := &Service{
		mode: Mode{
			ID:           ModeCoach,
			Name:         "Coach",
			Description:  "Test coach",
			Instructions: "Test instructions",
		},
	}

	messages, err := service.StartupMessages(context.Background(), SessionContext{})
	if err != nil {
		t.Fatalf("StartupMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 startup message, got %d", len(messages))
	}
	if messages[0] != text.CoachWelcomeMessage {
		t.Fatalf("expected coach welcome message, got %q", messages[0])
	}
}

func TestStartupMessagesForPerformanceReviewUseWelcomeMessage(t *testing.T) {
	service := &Service{
		mode: Mode{
			ID:           ModePerformanceReview,
			Name:         "Performance Review",
			Description:  "Test performance review",
			Instructions: "Test instructions",
		},
	}

	messages, err := service.StartupMessages(context.Background(), SessionContext{})
	if err != nil {
		t.Fatalf("StartupMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 startup message, got %d", len(messages))
	}
	if messages[0] != text.PerformanceReviewWelcomeMessage {
		t.Fatalf("expected performance review welcome message, got %q", messages[0])
	}
}

type toolCallingRoundTripper struct{}

func (toolCallingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	var payload chatRequest
	_ = json.Unmarshal(body, &payload)

	var response completionResponse
	switch len(payload.Messages) {
	case 4:
		response = completionResponse{
			Choices: []completionChoice{
				{
					Message: chatMessage{
						Role: "assistant",
						ToolCalls: []chatToolCall{
							{
								ID:   "call-1",
								Type: "function",
								Function: chatToolCallFunction{
									Name:      toolSearchMemories,
									Arguments: `{"query":"have we talked about this before?","limit":1}`,
								},
							},
						},
					},
				},
			},
		}
	default:
		response = completionResponse{
			Choices: []completionChoice{
				{
					Message: chatMessage{Role: "assistant", Content: "The user is asking whether this has come up before, and the memory search found a related prior session."},
				},
			},
		}
	}

	jsonData, _ := json.Marshal(response)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(jsonData)),
		Header:     make(http.Header),
	}, nil
}

func TestStartupMessagesWithSummaryIncludeGeneratedGuidance(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	client, err := NewClient(ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: startupRoundTripper{content: "We could begin with the recurring delay.\n- The pattern you keep repeating\n- The conflict you keep postponing"}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	service := &Service{
		client: client,
		mode: Mode{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
	}

	messages, err := service.StartupMessages(context.Background(), SessionContext{Summary: "## Overview\nRecurring delay around work."})
	if err != nil {
		t.Fatalf("StartupMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 startup message, got %d", len(messages))
	}
	if messages[0] != text.AnalystWelcomeMessage {
		t.Fatalf("expected welcome message, got %q", messages[0])
	}

	// Test the second message is loaded separately
	secondMessage, err := service.LoadAnalystSecondMessage(context.Background(), SessionContext{Summary: "## Overview\nRecurring delay around work."})
	if err != nil {
		t.Fatalf("LoadAnalystSecondMessage() error = %v", err)
	}
	if !strings.Contains(secondMessage, "recurring delay") {
		t.Fatalf("expected generated guidance, got %q", secondMessage)
	}
}

func TestStartupMessagesWithSummaryAddsRoleMappingGuardrails(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	personaPath, err := EnsurePersonaFile()
	if err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}
	if err := os.WriteFile(personaPath, []byte("Your name is Claudio.\n"), 0o644); err != nil {
		t.Fatalf("write persona: %v", err)
	}

	client, err := NewClient(ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient: &http.Client{Transport: startupInspectingRoundTripper{
			t:               t,
			responseContent: "We can begin with the unresolved loop around shipping.",
		}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	service := &Service{
		client: client,
		mode: Mode{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
	}

	_, err = service.LoadAnalystSecondMessage(context.Background(), SessionContext{Summary: "## Overview\nRecurring delay around work."})
	if err != nil {
		t.Fatalf("LoadAnalystSecondMessage() error = %v", err)
	}
}

func TestGenerateAnalysisUsesMemoryToolsWhenModelRequestsThem(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	client, err := NewClient(ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: toolCallingRoundTripper{}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nWork avoidance and fear of judgment.", Score: 9},
		},
	}

	service := &Service{
		client: client,
		mode: Mode{
			ID:           ModeAnalyst,
			Name:         "Analyst",
			Description:  "Test analyst",
			Instructions: "Test instructions",
		},
		store: store,
	}

	analysis, err := service.GenerateAnalysis(context.Background(), "have we talked about this before?", SessionContext{})
	if err != nil {
		t.Fatalf("GenerateAnalysis() error = %v", err)
	}
	if !strings.Contains(analysis, "related prior session") {
		t.Fatalf("expected tool-informed analysis, got %q", analysis)
	}
}
