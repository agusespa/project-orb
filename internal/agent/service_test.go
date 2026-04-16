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
	memories              []MemorySnippet
	transcriptMatches     []MemorySnippet
	transcriptExcerpt     string
	transcriptExact       MemoryTranscript
	summarySearchCalls    int
	transcriptSearchCalls int
	transcriptCalls       int
	exactTranscriptCalls  int
}

func (s *stubSessionStore) LoadLatestSummary(mode ModeID) (SessionContext, bool, error) {
	return NewSessionContext(), false, nil
}

func (s *stubSessionStore) SaveSession(ctx context.Context, mode ModeID, session SessionContext) error {
	return nil
}

func (s *stubSessionStore) SearchRelevantSummaries(mode ModeID, query string, excludeSummary string, limit int) ([]MemorySnippet, error) {
	s.summarySearchCalls++
	if len(s.memories) > limit {
		return s.memories[:limit], nil
	}
	return s.memories, nil
}

func (s *stubSessionStore) SearchTranscriptExcerpts(mode ModeID, query string, limit int, maxTurns int) ([]MemorySnippet, error) {
	s.transcriptSearchCalls++
	if len(s.transcriptMatches) > limit {
		return s.transcriptMatches[:limit], nil
	}
	return s.transcriptMatches, nil
}

func (s *stubSessionStore) LoadTranscript(mode ModeID, sessionID string) (MemoryTranscript, error) {
	s.exactTranscriptCalls++
	if strings.TrimSpace(s.transcriptExact.SessionID) == "" && strings.TrimSpace(s.transcriptExact.Transcript) == "" {
		return MemoryTranscript{SessionID: sessionID}, nil
	}
	return s.transcriptExact, nil
}

func (s *stubSessionStore) LoadTranscriptExcerpt(mode ModeID, sessionID string, query string, maxTurns int) (string, error) {
	s.transcriptCalls++
	return s.transcriptExcerpt, nil
}

func TestBuildConversationMessagesIncludesMemories(t *testing.T) {
	messages := buildConversationMessages("system", SessionContext{
		Summary: "Current summary",
		WorkingHistory: []Turn{
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

func TestAnalysisToolsOnlyForAnalysisMode(t *testing.T) {
	service := &Service{
		mode:  DefaultMode(),
		store: &stubSessionStore{memories: []MemorySnippet{{SessionID: "1", Summary: "Past memory", Score: 1}}},
	}

	if got := service.analysisTools("prompt", SessionContext{}); len(got) != 0 {
		t.Fatalf("expected no tools for non-analysis mode, got %d", len(got))
	}
}

func TestSearchMemoriesToolUsesStoreForAnalysisMode(t *testing.T) {
	store := &stubSessionStore{
		memories: []MemorySnippet{{SessionID: "1", Summary: "Past memory", Score: 8}},
	}
	service := &Service{
		mode: Mode{
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
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
		memories:          []MemorySnippet{{SessionID: "1", Summary: "Past memory", Score: 8}},
		transcriptExcerpt: "User: You said you were scared.\nAssistant: We traced it to fear of being judged.",
	}
	service := &Service{
		mode: Mode{
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
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

func TestSearchTranscriptMemoriesToolUsesRawTranscriptMatches(t *testing.T) {
	store := &stubSessionStore{
		transcriptMatches: []MemorySnippet{{
			SessionID: "2026-04-05-120000",
			Excerpt:   "User: where did the stop-doing list come from?\nAssistant: We named a stop-doing list in that session.",
			Score:     7,
		}},
	}
	service := &Service{
		mode: Mode{
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
			Instructions: "Test instructions",
		},
		store: store,
	}

	tool := service.searchTranscriptMemoriesTool("where did the stop-doing list come from?")
	got, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got, "stop-doing list") {
		t.Fatalf("expected raw transcript result in tool output, got %q", got)
	}
}

func TestLoadMemoryTranscriptToolLoadsLatestTranscript(t *testing.T) {
	store := &stubSessionStore{
		transcriptExact: MemoryTranscript{
			SessionID:  "2026-04-05-120000",
			Transcript: "# Session - 2026-04-05T12:00:00Z\n\n## Conversation\n### User\nTell me the raw text.\n\n### Assistant\nHere is what we said.",
		},
	}
	service := &Service{
		mode: Mode{
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
			Instructions: "Test instructions",
		},
		store: store,
	}

	tool := service.loadMemoryTranscriptTool()
	got, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(got, "\"session_id\":\"2026-04-05-120000\"") {
		t.Fatalf("expected resolved session id in tool output, got %q", got)
	}
	if !strings.Contains(got, "Tell me the raw text.") {
		t.Fatalf("expected exact transcript in tool output, got %q", got)
	}
	if store.exactTranscriptCalls != 1 {
		t.Fatalf("expected exactly one exact transcript load, got %d", store.exactTranscriptCalls)
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
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
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
	if messages[0] != text.AnalysisWelcomeMessage {
		t.Fatalf("expected welcome message, got %q", messages[0])
	}

	// Test the second message is loaded separately
	secondMessage, err := service.LoadAnalysisSecondMessage(context.Background(), SessionContext{})
	if err != nil {
		t.Fatalf("LoadAnalysisSecondMessage() error = %v", err)
	}
	if secondMessage != text.AnalysisFreshStartMessage {
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

type analysisInspectingRoundTripper struct {
	t                 *testing.T
	requiredFragments []string
	responseContent   string
}

func (a analysisInspectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	payload := string(body)
	for _, fragment := range a.requiredFragments {
		if !strings.Contains(payload, fragment) {
			a.t.Fatalf("expected analysis request to include %q, payload=%q", fragment, payload)
		}
	}

	response := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]string{
					"content": a.responseContent,
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
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
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
	if messages[0] != text.AnalysisWelcomeMessage {
		t.Fatalf("expected welcome message, got %q", messages[0])
	}

	// Test the second message is loaded separately
	secondMessage, err := service.LoadAnalysisSecondMessage(context.Background(), SessionContext{Summary: "## Overview\nRecurring delay around work."})
	if err != nil {
		t.Fatalf("LoadAnalysisSecondMessage() error = %v", err)
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
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
			Instructions: "Test instructions",
		},
	}

	_, err = service.LoadAnalysisSecondMessage(context.Background(), SessionContext{Summary: "## Overview\nRecurring delay around work."})
	if err != nil {
		t.Fatalf("LoadAnalysisSecondMessage() error = %v", err)
	}
}

func TestGenerateAnalysisUsesMemoryToolsWhenModelRequestsThem(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "memory-recall"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nWork avoidance and fear of judgment.", Score: 9},
		},
		transcriptExcerpt: "User: I was afraid of being judged when shipping.\nAssistant: We named the fear directly.",
	}

	service := &Service{
		client: client,
		mode:   BuiltInModes()[2],
		store:  store,
	}

	analysis, err := service.GenerateAnalysis(context.Background(), "have we talked about this before?", SessionContext{})
	if err != nil {
		t.Fatalf("GenerateAnalysis() error = %v", err)
	}
	if !strings.Contains(analysis, "prior session") {
		t.Fatalf("expected tool-informed analysis, got %q", analysis)
	}
}

func TestGenerateAnalysisPrefetchesLatestTranscriptForLastSessionRequests(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	client, err := NewClient(ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient: &http.Client{Transport: analysisInspectingRoundTripper{
			t:                 t,
			requiredFragments: []string{"Verified latest saved session transcript", "What did we talk about last time?"},
			responseContent:   "I reviewed the latest saved transcript before answering.",
		}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	store := &stubSessionStore{
		transcriptExact: MemoryTranscript{
			SessionID:  "2026-04-05-120000",
			Transcript: "# Session - 2026-04-05T12:00:00Z\n\n## Conversation\n### User\nWhat did we talk about last time?\n\n### Assistant\nWe focused on the stop-doing list.",
		},
	}

	service := &Service{
		client: client,
		mode: Mode{
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
			Instructions: "Test instructions",
		},
		store: store,
	}

	analysis, err := service.GenerateAnalysis(context.Background(), "what did we talk about last time?", SessionContext{})
	if err != nil {
		t.Fatalf("GenerateAnalysis() error = %v", err)
	}
	if !strings.Contains(analysis, "latest saved transcript") {
		t.Fatalf("expected transcript-informed analysis, got %q", analysis)
	}
	if store.exactTranscriptCalls != 1 {
		t.Fatalf("expected one latest-transcript prefetch, got %d", store.exactTranscriptCalls)
	}
}

func TestGenerateAnalysisPrefetchesRawEvidenceForProvenanceChallenges(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	client, err := NewClient(ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient: &http.Client{Transport: analysisInspectingRoundTripper{
			t:                 t,
			requiredFragments: []string{"Verified memory evidence", "stop-doing list"},
			responseContent:   "I checked the verified memory evidence before answering.",
		}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	store := &stubSessionStore{
		transcriptMatches: []MemorySnippet{{
			SessionID: "2026-04-05-120000",
			Excerpt:   "User: What should go on the stop-doing list?\nAssistant: Let's create a stop-doing list for the habits that keep derailing you.",
			Score:     8,
		}},
	}

	service := &Service{
		client: client,
		mode: Mode{
			ID:           ModeAnalysis,
			Name:         "Analysis",
			Description:  "Test analysis",
			Instructions: "Test instructions",
		},
		store: store,
	}

	analysis, err := service.GenerateAnalysis(context.Background(), "what is that stop-doing list coming from? i dont remember talking about it in previous sessions", SessionContext{})
	if err != nil {
		t.Fatalf("GenerateAnalysis() error = %v", err)
	}
	if !strings.Contains(analysis, "verified memory evidence") {
		t.Fatalf("expected evidence-informed analysis, got %q", analysis)
	}
	if store.transcriptSearchCalls == 0 {
		t.Fatal("expected raw transcript search to run")
	}
}
