package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
				Recent: []Turn{},
			},
			wantWord: 0,
		},
		{
			name: "single turn",
			session: SessionContext{
				Recent: []Turn{
					{User: "hello world", Assistant: "hi there"},
				},
			},
			wantWord: 4,
		},
		{
			name: "multiple turns",
			session: SessionContext{
				Recent: []Turn{
					{User: "one two three", Assistant: "four five"},
					{User: "six", Assistant: "seven eight nine"},
				},
			},
			wantWord: 9,
		},
		{
			name: "whitespace handling",
			session: SessionContext{
				Recent: []Turn{
					{User: "  hello   world  ", Assistant: "  test  "},
				},
			},
			wantWord: 3,
		},
		{
			name: "summary not counted",
			session: SessionContext{
				Summary: strings.Repeat("word ", 1000), // 1000 words in summary
				Recent: []Turn{
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

func TestServicePrepareSessionMultipleCompactions(t *testing.T) {
	compactionCount := 0
	var summaries []string
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		compactionCount++
		// Simulate summary growing with each compaction
		newSummary := existingSummary
		if newSummary != "" {
			newSummary += " | "
		}
		newSummary += fmt.Sprintf("compaction_%d", compactionCount)
		summaries = append(summaries, newSummary)
		return newSummary, nil
	})

	// Start with empty session
	session := SessionContext{
		Summary: "",
		Recent:  []Turn{},
	}

	// Simulate multiple rounds of conversation that each trigger compaction
	for round := 1; round <= 3; round++ {
		// Add enough turns to trigger compaction
		longText := strings.Repeat("word ", 300)
		for i := 0; i < 6; i++ {
			session.AddTurn(Turn{User: longText, Assistant: longText})
		}

		if err := service.PrepareSession(context.Background(), &session); err != nil {
			t.Fatalf("PrepareSession() round %d error = %v", round, err)
		}
	}

	if compactionCount != 3 {
		t.Fatalf("expected 3 compactions, got %d", compactionCount)
	}

	// Verify summary keeps growing
	if !strings.Contains(session.Summary, "compaction_1") {
		t.Fatal("expected summary to contain first compaction")
	}
	if !strings.Contains(session.Summary, "compaction_2") {
		t.Fatal("expected summary to contain second compaction")
	}
	if !strings.Contains(session.Summary, "compaction_3") {
		t.Fatal("expected summary to contain third compaction")
	}

	// This demonstrates the issue: summary grows unbounded
	summaryWordCount := countWords(session.Summary)
	t.Logf("Summary word count after 3 compactions: %d", summaryWordCount)
	t.Logf("Final summary: %s", session.Summary)
}

func TestServicePrepareSessionWithinLimit(t *testing.T) {
	called := false
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	session := SessionContext{
		Summary: "existing",
		Recent: []Turn{
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

	if got := len(session.Recent); got != 2 {
		t.Fatalf("expected 2 recent turns, got %d", got)
	}
}

func TestServicePrepareSessionCompactsOverflow(t *testing.T) {
	var gotExisting string
	var gotTurns []Turn
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		gotExisting = existingSummary
		gotTurns = append([]Turn(nil), turns...)
		return "new summary", nil
	})

	// Create turns with enough words to exceed maxRecentWords (2000)
	// Each turn has ~250 words, so 10 turns = ~2500 words
	longText := strings.Repeat("word ", 250)
	session := SessionContext{
		Summary: "old summary",
		Recent: []Turn{
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
	if initialWordCount <= maxRecentWords {
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

	if len(session.Recent) < minRecentTurns {
		t.Fatalf("expected at least %d recent turns after compaction, got %d", minRecentTurns, len(session.Recent))
	}

	if session.WordCount() > maxRecentWords {
		t.Fatalf("expected word count under %d after compaction, got %d", maxRecentWords, session.WordCount())
	}
}

func TestServicePrepareSessionExactlyAtLimit(t *testing.T) {
	called := false
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	// Create exactly maxRecentWords (3000 words)
	wordsPerTurn := maxRecentWords / 6 // 500 words per turn, 3 turns = 3000 words
	text := strings.Repeat("word ", wordsPerTurn)
	session := SessionContext{
		Summary: "existing",
		Recent: []Turn{
			{User: text, Assistant: text},
			{User: text, Assistant: text},
			{User: text, Assistant: text},
		},
	}

	wordCount := session.WordCount()
	if wordCount != maxRecentWords {
		t.Fatalf("test setup error: expected exactly %d words, got %d", maxRecentWords, wordCount)
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if called {
		t.Fatal("expected summarizer not to be called when exactly at limit")
	}

	if session.Summary != "existing" {
		t.Fatalf("expected summary unchanged, got %q", session.Summary)
	}
}

func TestServicePrepareSessionOneWordOverLimit(t *testing.T) {
	var gotTurns []Turn
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		gotTurns = append([]Turn(nil), turns...)
		return "updated summary", nil
	})

	// Create more than minRecentTurns with enough words to exceed limit
	// Use 400 words per turn, 6 turns = 4800 words (over 3000 limit)
	wordsPerTurn := 400
	text := strings.Repeat("word ", wordsPerTurn)
	session := SessionContext{
		Summary: "old",
		Recent: []Turn{
			{User: text, Assistant: text},
			{User: text, Assistant: text},
			{User: text, Assistant: text},
			{User: text, Assistant: text},
			{User: text, Assistant: text},
			{User: text, Assistant: text},
		},
	}

	initialWordCount := session.WordCount()
	if initialWordCount <= maxRecentWords {
		t.Fatalf("test setup error: expected over %d words, got %d", maxRecentWords, initialWordCount)
	}

	if len(session.Recent) <= minRecentTurns {
		t.Fatalf("test setup error: need more than %d turns, got %d", minRecentTurns, len(session.Recent))
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if len(gotTurns) == 0 {
		t.Fatal("expected at least one overflow turn")
	}

	if len(session.Recent) < minRecentTurns {
		t.Fatalf("expected at least %d recent turns after compaction, got %d", minRecentTurns, len(session.Recent))
	}

	if session.WordCount() > maxRecentWords {
		t.Fatalf("expected word count under %d after compaction, got %d", maxRecentWords, session.WordCount())
	}

	if session.Summary != "updated summary" {
		t.Fatalf("expected updated summary, got %q", session.Summary)
	}
}

func TestServicePrepareSessionEmptySession(t *testing.T) {
	called := false
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	session := SessionContext{
		Summary: "",
		Recent:  []Turn{},
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if called {
		t.Fatal("expected summarizer not to be called for empty session")
	}

	if len(session.Recent) != 0 {
		t.Fatalf("expected 0 recent turns, got %d", len(session.Recent))
	}
}

func TestServicePrepareSessionNoExistingSummary(t *testing.T) {
	var gotExisting string
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		gotExisting = existingSummary
		return "first summary", nil
	})

	// Create turns with enough words to exceed limit (400 words per turn, 6 turns = 4800 words)
	longText := strings.Repeat("word ", 400)
	session := SessionContext{
		Summary: "",
		Recent: []Turn{
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
		},
	}

	if session.WordCount() <= maxRecentWords {
		t.Fatalf("test setup error: expected over %d words, got %d", maxRecentWords, session.WordCount())
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if gotExisting != "" {
		t.Fatalf("expected empty existing summary, got %q", gotExisting)
	}

	if session.Summary != "first summary" {
		t.Fatalf("expected new summary, got %q", session.Summary)
	}
}

func TestServicePrepareSessionRespectsMinRecentTurns(t *testing.T) {
	called := false
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	// Create exactly minRecentTurns (3) with high word count
	longText := strings.Repeat("word ", 1500)
	session := SessionContext{
		Summary: "existing",
		Recent: []Turn{
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
		},
	}

	if len(session.Recent) != minRecentTurns {
		t.Fatalf("test setup error: expected exactly %d turns, got %d", minRecentTurns, len(session.Recent))
	}

	if session.WordCount() <= maxRecentWords {
		t.Fatalf("test setup error: expected over %d words, got %d", maxRecentWords, session.WordCount())
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if called {
		t.Fatal("expected summarizer not to be called when at minRecentTurns")
	}

	if len(session.Recent) != minRecentTurns {
		t.Fatalf("expected %d recent turns preserved, got %d", minRecentTurns, len(session.Recent))
	}
}

func TestServicePrepareSessionPropagatesSummaryError(t *testing.T) {
	wantErr := errors.New("summary failed")
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		return "", wantErr
	})

	// Create turns with enough words to exceed limit (400 words per turn, 6 turns = 4800 words)
	longText := strings.Repeat("word ", 400)
	session := SessionContext{
		Recent: []Turn{
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

func TestServicePrepareSessionPassesContextToSummarizer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(gotCtx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		if !errors.Is(gotCtx.Err(), context.Canceled) {
			t.Fatalf("expected canceled context, got %v", gotCtx.Err())
		}

		return "", gotCtx.Err()
	})

	// Create turns with enough words to exceed limit (400 words per turn, 6 turns = 4800 words)
	longText := strings.Repeat("word ", 400)
	session := SessionContext{
		Recent: []Turn{
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
			{User: longText, Assistant: longText},
		},
	}

	err := service.PrepareSession(ctx, &session)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
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

func TestBuildConversationMessagesIncludesSummaryAndTurns(t *testing.T) {
	session := SessionContext{
		Summary: "user is stuck between two jobs",
		Recent: []Turn{
			{User: "I am torn.", Assistant: "What feels most important?"},
		},
	}

	messages := buildConversationMessages("system prompt", session)

	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}

	if messages[0].Role != "system" || messages[0].Content != "system prompt" {
		t.Fatalf("unexpected system message: %+v", messages[0])
	}

	if messages[1].Role != "user" || !strings.Contains(messages[1].Content, session.Summary) {
		t.Fatalf("expected summary message, got %+v", messages[1])
	}

	if messages[2].Role != "user" || messages[2].Content != "I am torn." {
		t.Fatalf("unexpected user turn: %+v", messages[2])
	}

	if messages[3].Role != "assistant" || messages[3].Content != "What feels most important?" {
		t.Fatalf("unexpected assistant turn: %+v", messages[3])
	}
}

func TestResolveAppConfigDirUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/project-orb-config")

	dir, err := resolveAppConfigDir()
	if err != nil {
		t.Fatalf("resolveAppConfigDir() error = %v", err)
	}

	want := filepath.Join("/tmp/project-orb-config", appName)
	if dir != want {
		t.Fatalf("expected %q, got %q", want, dir)
	}
}

func TestEnsurePersonaFileCreatesDefaultPersona(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	personaPath, err := EnsurePersonaFile()
	if err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	wantPath := filepath.Join(tempDir, appName, personaFile)
	if personaPath != wantPath {
		t.Fatalf("expected persona path %q, got %q", wantPath, personaPath)
	}

	data, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", personaPath, err)
	}

	if string(data) != defaultPersona {
		t.Fatalf("expected default persona contents to be written")
	}
}

func TestExtractCoachName(t *testing.T) {
	tests := []struct {
		name    string
		persona string
		want    string
	}{
		{
			name: "name field",
			persona: `Name: Sage
You are a calm coach.`,
			want: "Sage",
		},
		{
			name: "markdown bullet name field",
			persona: `- name: Coachy
- tone: grounded`,
			want: "Coachy",
		},
		{
			name: "your name is",
			persona: `Your name is Rowan.
You are a practical coach.`,
			want: "Rowan",
		},
		{
			name: "you are named entity",
			persona: `You are Solace.
Help me think clearly.`,
			want: "Solace",
		},
		{
			name:    "generic persona has no explicit name",
			persona: `You are a calm, practical AI life coach.`,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractCoachName(tt.persona); got != tt.want {
				t.Fatalf("ExtractCoachName() = %q, want %q", got, tt.want)
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

func TestSessionContextTotalWordCount(t *testing.T) {
	tests := []struct {
		name      string
		session   SessionContext
		wantTotal int
	}{
		{
			name: "empty session",
			session: SessionContext{
				Recent: []Turn{},
			},
			wantTotal: 0,
		},
		{
			name: "only recent turns",
			session: SessionContext{
				Recent: []Turn{
					{User: "hello world", Assistant: "hi there"},
				},
			},
			wantTotal: 4,
		},
		{
			name: "only summary",
			session: SessionContext{
				Summary: "one two three four five",
				Recent:  []Turn{},
			},
			wantTotal: 5,
		},
		{
			name: "both summary and recent",
			session: SessionContext{
				Summary: "one two three",
				Recent: []Turn{
					{User: "four five", Assistant: "six"},
				},
			},
			wantTotal: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.TotalWordCount()
			if got != tt.wantTotal {
				t.Fatalf("TotalWordCount() = %d, want %d", got, tt.wantTotal)
			}
		})
	}
}

func TestSessionContextUsagePercent(t *testing.T) {
	tests := []struct {
		name        string
		session     SessionContext
		wantPercent float64
	}{
		{
			name: "empty session",
			session: SessionContext{
				Recent: []Turn{},
			},
			wantPercent: 0,
		},
		{
			name: "10% usage",
			session: SessionContext{
				Summary: strings.Repeat("word ", 600), // 600 words = 10% of 6000
				Recent:  []Turn{},
			},
			wantPercent: 10,
		},
		{
			name: "50% usage",
			session: SessionContext{
				Summary: strings.Repeat("word ", 3000), // 3000 words = 50% of 6000
				Recent:  []Turn{},
			},
			wantPercent: 50,
		},
		{
			name: "100% usage",
			session: SessionContext{
				Summary: strings.Repeat("word ", 6000), // 6000 words = 100%
				Recent:  []Turn{},
			},
			wantPercent: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.ContextUsagePercent()
			if got != tt.wantPercent {
				t.Fatalf("ContextUsagePercent() = %.2f, want %.2f", got, tt.wantPercent)
			}
		})
	}
}
