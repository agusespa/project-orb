package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"project-orb/internal/paths"
)

type stubEmbedder struct {
	vectors map[string][]float64
}

func (s stubEmbedder) Embed(ctx context.Context, input string) ([]float64, error) {
	if vector, ok := s.vectors[strings.TrimSpace(input)]; ok {
		return vector, nil
	}
	return nil, os.ErrNotExist
}

func TestFileSessionStoreSaveAndLoadLatestSummary(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	store, err := NewFileSessionStore()
	if err != nil {
		t.Fatalf("NewFileSessionStore() error = %v", err)
	}

	session := NewSessionContext()
	session.Summary = "## Overview\nA saved analyst summary."
	session.Recent = []Turn{
		{
			User:      "I keep avoiding this project.",
			Assistant: "Let's look at what the avoidance is protecting.",
			CreatedAt: time.Now().UTC(),
		},
	}

	if err := store.SaveSession(context.Background(), ModeAnalyst, session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	loaded, found, err := store.LoadLatestSummary(ModeAnalyst)
	if err != nil {
		t.Fatalf("LoadLatestSummary() error = %v", err)
	}
	if !found {
		t.Fatal("expected saved summary to be found")
	}
	if got := strings.TrimSpace(loaded.Summary); got != strings.TrimSpace(session.Summary) {
		t.Fatalf("loaded summary = %q, want %q", got, session.Summary)
	}

	summaryPath, err := paths.AnalysisSessionsPath(string(ModeAnalyst), session.SessionID+"-summary.md")
	if err != nil {
		t.Fatalf("AnalysisSessionsPath() error = %v", err)
	}
	if _, err := os.Stat(summaryPath); err != nil {
		t.Fatalf("expected summary file at %s: %v", summaryPath, err)
	}
}

func TestFileSessionStoreSkipsEmptySession(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	store, err := NewFileSessionStore()
	if err != nil {
		t.Fatalf("NewFileSessionStore() error = %v", err)
	}

	session := NewSessionContext()
	session.Summary = "Existing summary with no new turns."

	if err := store.SaveSession(context.Background(), ModeAnalyst, session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	modeDir, err := paths.AnalysisSessionsPath(string(ModeAnalyst))
	if err != nil {
		t.Fatalf("AnalysisSessionsPath() error = %v", err)
	}
	if _, err := os.Stat(modeDir); !os.IsNotExist(err) {
		t.Fatalf("expected no session directory to be created, got err=%v", err)
	}
}

func TestFileSessionStoreSearchRelevantSummaries(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	store := &FileSessionStore{
		sessionsDir: mustAnalysisSessionsPath(t),
		embedder: stubEmbedder{
			vectors: map[string][]float64{
				"I am avoiding shipping this work because of anxiety":                                                             {1, 0},
				"## Overview\nAvoidance around work and anxiety about shipping.\n\n## Patterns\nPerfectionism and delay.":         {1, 0},
				"## Overview\nConflict with a friend and guilt after the conversation.\n\n## Patterns\nWithdrawal after tension.": {0, 1},
			},
		},
	}

	first := NewSessionContext()
	first.SessionID = "2026-04-05-120000"
	first.Summary = "## Overview\nAvoidance around work and anxiety about shipping.\n\n## Patterns\nPerfectionism and delay."
	first.Recent = []Turn{{User: "u", Assistant: "a", CreatedAt: time.Now().UTC()}}

	second := NewSessionContext()
	second.SessionID = "2026-04-05-130000"
	second.Summary = "## Overview\nConflict with a friend and guilt after the conversation.\n\n## Patterns\nWithdrawal after tension."
	second.Recent = []Turn{{User: "u", Assistant: "a", CreatedAt: time.Now().UTC()}}

	if err := store.SaveSession(context.Background(), ModeAnalyst, first); err != nil {
		t.Fatalf("SaveSession(first) error = %v", err)
	}
	if err := store.SaveSession(context.Background(), ModeAnalyst, second); err != nil {
		t.Fatalf("SaveSession(second) error = %v", err)
	}

	results, err := store.SearchRelevantSummaries(ModeAnalyst, "I am avoiding shipping this work because of anxiety", "", 2)
	if err != nil {
		t.Fatalf("SearchRelevantSummaries() error = %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one relevant summary")
	}
	if results[0].SessionID != first.SessionID {
		t.Fatalf("expected most relevant result %s, got %s", first.SessionID, results[0].SessionID)
	}

	excluded, err := store.SearchRelevantSummaries(ModeAnalyst, "I am avoiding shipping this work because of anxiety", first.Summary, 2)
	if err != nil {
		t.Fatalf("SearchRelevantSummaries() with exclude error = %v", err)
	}
	for _, result := range excluded {
		if strings.TrimSpace(result.Summary) == strings.TrimSpace(first.Summary) {
			t.Fatal("expected excluded summary not to appear in results")
		}
	}
}

func TestFileSessionStoreSkipsSearchWhenEmbeddingsUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	store := &FileSessionStore{
		sessionsDir: mustAnalysisSessionsPath(t),
		embedder:    nil,
	}

	modeDir := mustAnalysisSessionsPath(t, string(ModeAnalyst))
	if err := os.MkdirAll(modeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	summary := "## Overview\nAvoidance around work and anxiety about shipping.\n\n## Patterns\nPerfectionism and delay."
	if err := os.WriteFile(mustAnalysisSessionsPath(t, string(ModeAnalyst), "2026-04-05-120000-summary.md"), []byte(renderSummaryMarkdown(ModeAnalyst, SessionContext{Summary: summary})), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	results, err := store.SearchRelevantSummaries(ModeAnalyst, "work anxiety", "", 3)
	if err != nil {
		t.Fatalf("SearchRelevantSummaries() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results without embeddings, got %d", len(results))
	}
}

func TestFileSessionStoreUsesSemanticScoresWhenAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	store := &FileSessionStore{
		sessionsDir: mustAnalysisSessionsPath(t),
		embedder: stubEmbedder{
			vectors: map[string][]float64{
				"work anxiety": {1, 0},
				"## Overview\nWork anxiety and avoidance.\n\n## Patterns\nDelay before shipping.":   {1, 0},
				"## Overview\nFriend conflict and guilt.\n\n## Patterns\nWithdrawal after tension.": {0, 1},
			},
		},
	}

	modeDir := mustAnalysisSessionsPath(t, string(ModeAnalyst))
	if err := os.MkdirAll(modeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	firstSummary := "## Overview\nWork anxiety and avoidance.\n\n## Patterns\nDelay before shipping."
	secondSummary := "## Overview\nFriend conflict and guilt.\n\n## Patterns\nWithdrawal after tension."

	if err := os.WriteFile(mustAnalysisSessionsPath(t, string(ModeAnalyst), "2026-04-05-120000-summary.md"), []byte(renderSummaryMarkdown(ModeAnalyst, SessionContext{Summary: firstSummary})), 0o644); err != nil {
		t.Fatalf("write first summary: %v", err)
	}
	if err := os.WriteFile(mustAnalysisSessionsPath(t, string(ModeAnalyst), "2026-04-05-130000-summary.md"), []byte(renderSummaryMarkdown(ModeAnalyst, SessionContext{Summary: secondSummary})), 0o644); err != nil {
		t.Fatalf("write second summary: %v", err)
	}

	firstEmbedding, _ := json.Marshal([]float64{1, 0})
	secondEmbedding, _ := json.Marshal([]float64{0, 1})
	if err := os.WriteFile(mustAnalysisSessionsPath(t, string(ModeAnalyst), "2026-04-05-120000-summary.embedding.json"), firstEmbedding, 0o644); err != nil {
		t.Fatalf("write first embedding: %v", err)
	}
	if err := os.WriteFile(mustAnalysisSessionsPath(t, string(ModeAnalyst), "2026-04-05-130000-summary.embedding.json"), secondEmbedding, 0o644); err != nil {
		t.Fatalf("write second embedding: %v", err)
	}

	results, err := store.SearchRelevantSummaries(ModeAnalyst, "work anxiety", "", 2)
	if err != nil {
		t.Fatalf("SearchRelevantSummaries() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected semantic results")
	}
	if results[0].SessionID != "2026-04-05-120000" {
		t.Fatalf("expected semantic match to rank first, got %s", results[0].SessionID)
	}
}

func TestFileSessionStoreLoadTranscriptExcerpt(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	store := &FileSessionStore{
		sessionsDir: mustAnalysisSessionsPath(t),
	}

	modeDir := mustAnalysisSessionsPath(t, string(ModeAnalyst))
	if err := os.MkdirAll(modeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	session := SessionContext{
		StartedAt: time.Now().UTC(),
		Recent: []Turn{
			{User: "I keep delaying shipping because I am scared of being judged.", Assistant: "That sounds like fear, not confusion."},
			{User: "What if they think I am not good enough?", Assistant: "That is the part we should keep naming directly."},
		},
	}
	if err := os.WriteFile(mustAnalysisSessionsPath(t, string(ModeAnalyst), "2026-04-05-120000-session.md"), []byte(renderSessionMarkdown(ModeAnalyst, session)), 0o644); err != nil {
		t.Fatalf("write session transcript: %v", err)
	}

	excerpt, err := store.LoadTranscriptExcerpt(ModeAnalyst, "2026-04-05-120000", "What exactly was I afraid would happen?", 1)
	if err != nil {
		t.Fatalf("LoadTranscriptExcerpt() error = %v", err)
	}
	if !strings.Contains(excerpt, "What if they think I am not good enough?") {
		t.Fatalf("expected excerpt to include the most relevant turn, got %q", excerpt)
	}
}

func mustAnalysisSessionsPath(t *testing.T, parts ...string) string {
	t.Helper()

	path, err := paths.AnalysisSessionsPath(parts...)
	if err != nil {
		t.Fatalf("AnalysisSessionsPath() error = %v", err)
	}

	return path
}
