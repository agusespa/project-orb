package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServicePrepareSessionWithinLimit(t *testing.T) {
	called := false
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		called = true
		return "", nil
	})

	session := SessionContext{
		Summary: "existing",
		Recent: []Turn{
			{User: "u1", Assistant: "a1"},
			{User: "u2", Assistant: "a2"},
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

	session := SessionContext{
		Summary: "old summary",
		Recent: []Turn{
			{User: "u1", Assistant: "a1"},
			{User: "u2", Assistant: "a2"},
			{User: "u3", Assistant: "a3"},
			{User: "u4", Assistant: "a4"},
			{User: "u5", Assistant: "a5"},
			{User: "u6", Assistant: "a6"},
			{User: "u7", Assistant: "a7"},
			{User: "u8", Assistant: "a8"},
		},
	}

	if err := service.PrepareSession(context.Background(), &session); err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}

	if gotExisting != "old summary" {
		t.Fatalf("expected existing summary to be passed through, got %q", gotExisting)
	}

	if len(gotTurns) != 2 {
		t.Fatalf("expected 2 overflow turns, got %d", len(gotTurns))
	}

	if session.Summary != "new summary" {
		t.Fatalf("expected updated summary, got %q", session.Summary)
	}

	if len(session.Recent) != maxRecentTurns {
		t.Fatalf("expected %d recent turns after compaction, got %d", maxRecentTurns, len(session.Recent))
	}

	if session.Recent[0].User != "u3" {
		t.Fatalf("expected recent history to retain newest turns, got first user %q", session.Recent[0].User)
	}
}

func TestServicePrepareSessionPropagatesSummaryError(t *testing.T) {
	wantErr := errors.New("summary failed")
	service := newServiceWithSummarizer(testClient(), DefaultMode(), func(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
		return "", wantErr
	})

	session := SessionContext{
		Recent: []Turn{
			{User: "u1", Assistant: "a1"},
			{User: "u2", Assistant: "a2"},
			{User: "u3", Assistant: "a3"},
			{User: "u4", Assistant: "a4"},
			{User: "u5", Assistant: "a5"},
			{User: "u6", Assistant: "a6"},
			{User: "u7", Assistant: "a7"},
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

	session := SessionContext{
		Recent: []Turn{
			{User: "u1", Assistant: "a1"},
			{User: "u2", Assistant: "a2"},
			{User: "u3", Assistant: "a3"},
			{User: "u4", Assistant: "a4"},
			{User: "u5", Assistant: "a5"},
			{User: "u6", Assistant: "a6"},
			{User: "u7", Assistant: "a7"},
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
