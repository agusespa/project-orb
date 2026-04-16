package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSessionContextWordCount(t *testing.T) {
	tests := []struct {
		name     string
		session  SessionContext
		wantWord int
	}{
		{
			name: "empty session",
			session: SessionContext{
				WorkingHistory: []Turn{},
			},
			wantWord: 0,
		},
		{
			name: "single turn",
			session: SessionContext{
				WorkingHistory: []Turn{
					{User: "hello world", Assistant: "hi there"},
				},
			},
			wantWord: 4,
		},
		{
			name: "multiple turns",
			session: SessionContext{
				WorkingHistory: []Turn{
					{User: "one two three", Assistant: "four five"},
					{User: "six", Assistant: "seven eight nine"},
				},
			},
			wantWord: 9,
		},
		{
			name: "whitespace handling",
			session: SessionContext{
				WorkingHistory: []Turn{
					{User: "  hello   world  ", Assistant: "  test  "},
				},
			},
			wantWord: 3,
		},
		{
			name: "summary not counted",
			session: SessionContext{
				Summary: strings.Repeat("word ", 1000), // 1000 words in summary
				WorkingHistory: []Turn{
					{User: "hello", Assistant: "world"},
				},
			},
			wantWord: 2, // Only counts Recent, not Summary
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.WordCount()
			if got != tt.wantWord {
				t.Fatalf("WordCount() = %d, want %d", got, tt.wantWord)
			}
		})
	}
}

func TestServicePrepareSessionWithinLimit(t *testing.T) {
	called := false
	service := newServiceWithCompactor(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	session := SessionContext{
		Summary: "existing",
		WorkingHistory: []Turn{
			{User: "short", Assistant: "reply"},
			{User: "another", Assistant: "response"},
		},
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if called {
		t.Fatal("expected summarizer not to be called for short history")
	}

	if got := len(session.WorkingHistory); got != 2 {
		t.Fatalf("expected 2 recent turns, got %d", got)
	}
}

func TestServicePrepareSessionCompactsOverflow(t *testing.T) {
	var gotExisting string
	var gotTurns []Turn
	service := newServiceWithCompactor(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		gotExisting = existingSummary
		gotTurns = append([]Turn(nil), turns...)
		return "new summary", nil
	})

	// Create turns with enough words to exceed maxWorkingHistoryWords (2000)
	// Each turn has ~250 words, so 10 turns = ~2500 words
	longText := strings.Repeat("word ", 250)
	session := SessionContext{
		Summary: "old summary",
		WorkingHistory: []Turn{
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
		},
	}

	initialWordCount := session.WordCount()
	if initialWordCount <= maxWorkingHistoryWords {
		t.Fatalf("test setup error: session should exceed word limit, got %d words", initialWordCount)
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if gotExisting != "old summary" {
		t.Fatalf("expected existing summary to be passed through, got %q", gotExisting)
	}

	if len(gotTurns) == 0 {
		t.Fatal("expected some overflow turns to be compacted")
	}

	if session.Summary != "new summary" {
		t.Fatalf("expected updated summary, got %q", session.Summary)
	}

	if len(session.WorkingHistory) < minWorkingHistoryTurns {
		t.Fatalf("expected at least %d recent turns after compaction, got %d", minWorkingHistoryTurns, len(session.WorkingHistory))
	}

	if session.WordCount() > maxWorkingHistoryWords {
		t.Fatalf("expected word count under %d after compaction, got %d", maxWorkingHistoryWords, session.WordCount())
	}
}

func TestServicePrepareSessionRecompactsOversizedSummary(t *testing.T) {
	called := false
	service := newServiceWithCompactor(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		if len(turns) != 0 {
			t.Fatalf("expected summary-only compaction, got %d turns", len(turns))
		}
		return "compressed summary", nil
	})

	session := SessionContext{
		Summary: strings.Repeat("summary ", maxTotalWords+50),
		WorkingHistory: []Turn{
			{User: "short", Assistant: "reply"},
			{User: "still short", Assistant: "still fine"},
		},
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if !called {
		t.Fatal("expected oversized summary to be re-compacted")
	}
	if session.Summary != "compressed summary" {
		t.Fatalf("expected summary to be replaced, got %q", session.Summary)
	}
}

func TestServicePrepareSessionEmptySession(t *testing.T) {
	called := false
	service := newServiceWithCompactor(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	session := SessionContext{
		Summary:        "",
		WorkingHistory: []Turn{},
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if called {
		t.Fatal("expected summarizer not to be called for empty session")
	}

	if len(session.WorkingHistory) != 0 {
		t.Fatalf("expected 0 recent turns, got %d", len(session.WorkingHistory))
	}
}

func TestServicePrepareSessionPropagatesSummaryError(t *testing.T) {
	wantErr := errors.New("summary failed")
	service := newServiceWithCompactor(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		return "", wantErr
	})

	// Create turns with enough words to exceed limit (400 words per turn, 6 turns = 4800 words)
	longText := strings.Repeat("word ", 400)
	session := SessionContext{
		WorkingHistory: []Turn{
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
		},
	}

	err := service.PrepareSession(context.Background(), &session)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestNewServiceRejectsNilClient(t *testing.T) {
	service, err := NewService(nil, DefaultMode())
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if service != nil {
		t.Fatal("expected nil service on error")
	}
}

func TestExtractAgentName(t *testing.T) {
	tests := []struct {
		name    string
		persona string
		want    string
	}{
		{
			name: "standard format with name",
			persona: `# Persona

Your name is Rowan.

You are a practical agent.`,
			want: "Rowan",
		},
		{
			name: "standard format without name",
			persona: `# Persona

You are calm, thoughtful, and supportive.`,
			want: "",
		},
		{
			name: "name with special characters",
			persona: `Your name is Mary-Jane O'Brien.

You are helpful.`,
			want: "Mary-Jane O'Brien",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractAgentName(tt.persona); got != tt.want {
				t.Fatalf("ExtractAgentName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testClient() *Client {
	return &Client{
		completionsURL: "http://example.invalid/v1/chat/completions",
		model:          "test-model",
		httpClient:     &http.Client{},
	}
}
