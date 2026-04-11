package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"project-orb/internal/paths"
	"project-orb/internal/text"
)

type SessionStore interface {
	LoadLatestSummary(mode ModeID) (SessionContext, bool, error)
	SaveSession(ctx context.Context, mode ModeID, session SessionContext) error
	SearchRelevantSummaries(mode ModeID, query string, excludeSummary string, limit int) ([]MemorySnippet, error)
	LoadTranscriptExcerpt(mode ModeID, sessionID string, query string, maxTurns int) (string, error)
}

type FileSessionStore struct {
	sessionsDir string
	embedder    embedder
}

func NewFileSessionStore() (*FileSessionStore, error) {
	sessionsDir, err := paths.AnalysisSessionsPath()
	if err != nil {
		return nil, err
	}

	return &FileSessionStore{
		sessionsDir: sessionsDir,
		embedder:    NewEmbeddingClient(),
	}, nil
}

func (s *FileSessionStore) LoadLatestSummary(mode ModeID) (SessionContext, bool, error) {
	dir := s.modeDir(mode)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return NewSessionContext(), false, nil
		}
		return SessionContext{}, false, fmt.Errorf("read session dir: %w", err)
	}

	var summaryFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "-summary.md") {
			summaryFiles = append(summaryFiles, filepath.Join(dir, name))
		}
	}

	if len(summaryFiles) == 0 {
		return NewSessionContext(), false, nil
	}

	sort.Strings(summaryFiles)
	latestPath := summaryFiles[len(summaryFiles)-1]
	data, err := os.ReadFile(latestPath)
	if err != nil {
		return SessionContext{}, false, fmt.Errorf("read summary file: %w", err)
	}

	session := NewSessionContext()
	session.Summary = extractSummaryBody(string(data))
	return session, strings.TrimSpace(session.Summary) != "", nil
}

func (s *FileSessionStore) SaveSession(ctx context.Context, mode ModeID, session SessionContext) error {
	session.EnsureMetadata()

	if len(session.Recent) == 0 {
		return nil
	}

	dir := s.modeDir(mode)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	sessionPath := filepath.Join(dir, session.SessionID+"-session.md")
	summaryPath := filepath.Join(dir, session.SessionID+"-summary.md")
	embeddingPath := filepath.Join(dir, session.SessionID+"-summary.embedding.json")

	if err := os.WriteFile(sessionPath, []byte(renderSessionMarkdown(mode, session)), 0o644); err != nil {
		return fmt.Errorf("write session transcript: %w", err)
	}
	if err := os.WriteFile(summaryPath, []byte(renderSummaryMarkdown(mode, session)), 0o644); err != nil {
		return fmt.Errorf("write session summary: %w", err)
	}
	s.saveEmbeddingBestEffort(ctx, embeddingPath, session.Summary)

	return nil
}

func (s *FileSessionStore) SearchRelevantSummaries(mode ModeID, query string, excludeSummary string, limit int) ([]MemorySnippet, error) {
	if limit <= 0 {
		return nil, nil
	}

	if s.embedder == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}

	queryVector, err := s.embedder.Embed(context.Background(), query)
	if err != nil || len(queryVector) == 0 {
		return nil, nil
	}

	dir := s.modeDir(mode)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session dir: %w", err)
	}

	var snippets []MemorySnippet
	excluded := strings.TrimSpace(excludeSummary)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, "-summary.md") {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read summary file: %w", err)
		}

		summary := extractSummaryBody(string(data))
		if strings.TrimSpace(summary) == "" || strings.TrimSpace(summary) == excluded {
			continue
		}

		embeddingPath := filepath.Join(dir, strings.TrimSuffix(name, "-summary.md")+"-summary.embedding.json")
		score, ok := s.semanticScore(queryVector, embeddingPath)
		if !ok {
			continue
		}

		sessionID := strings.TrimSuffix(name, "-summary.md")
		snippets = append(snippets, MemorySnippet{
			SessionID: sessionID,
			Summary:   summary,
			Score:     score,
		})
	}

	sort.SliceStable(snippets, func(i, j int) bool {
		if snippets[i].Score == snippets[j].Score {
			return snippets[i].SessionID > snippets[j].SessionID
		}
		return snippets[i].Score > snippets[j].Score
	})

	if len(snippets) > limit {
		snippets = snippets[:limit]
	}

	return snippets, nil
}

func (s *FileSessionStore) saveEmbeddingBestEffort(ctx context.Context, path string, summary string) {
	if s.embedder == nil || strings.TrimSpace(summary) == "" {
		return
	}

	vector, err := s.embedder.Embed(ctx, summary)
	if err != nil {
		return
	}

	data, err := json.Marshal(vector)
	if err != nil {
		return
	}

	_ = os.WriteFile(path, data, 0o644)
}

func (s *FileSessionStore) LoadTranscriptExcerpt(mode ModeID, sessionID string, query string, maxTurns int) (string, error) {
	if maxTurns <= 0 || strings.TrimSpace(sessionID) == "" {
		return "", nil
	}

	path := filepath.Join(s.modeDir(mode), sessionID+"-session.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read session transcript: %w", err)
	}

	turns := extractTranscriptTurns(string(data))
	if len(turns) == 0 {
		return "", nil
	}

	best := pickRelevantTurns(turns, query, maxTurns)
	if len(best) == 0 {
		return "", nil
	}

	var b strings.Builder
	for i, turn := range best {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if user := strings.TrimSpace(turn.User); user != "" {
			b.WriteString(text.SessionUserPrefix)
			b.WriteString(user)
			b.WriteString("\n")
		}
		if assistant := strings.TrimSpace(turn.Assistant); assistant != "" {
			b.WriteString(text.SessionAssistantPrefix)
			b.WriteString(assistant)
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func (s *FileSessionStore) semanticScore(queryVector []float64, embeddingPath string) (int, bool) {
	data, err := os.ReadFile(embeddingPath)
	if err != nil {
		return 0, false
	}

	var summaryVector []float64
	if err := json.Unmarshal(data, &summaryVector); err != nil {
		return 0, false
	}

	score := cosineSimilarity(queryVector, summaryVector)
	if score <= 0 {
		return 0, false
	}

	return int(math.Round(score * 10)), true
}

func (s *FileSessionStore) modeDir(mode ModeID) string {
	return filepath.Join(s.sessionsDir, string(mode))
}

func renderSessionMarkdown(mode ModeID, session SessionContext) string {
	var b strings.Builder

	timestamp := session.StartedAt.Format(time.RFC3339)
	if session.StartedAt.IsZero() {
		timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	b.WriteString("# Session - ")
	b.WriteString(timestamp)
	b.WriteString("\n\n")
	b.WriteString("## Mode\n")
	b.WriteString(string(mode))
	b.WriteString("\n\n")
	b.WriteString("## Summary\n")
	if strings.TrimSpace(session.Summary) != "" {
		b.WriteString(strings.TrimSpace(session.Summary))
		b.WriteString("\n")
	} else {
		b.WriteString("(none)\n")
	}
	b.WriteString("\n## Conversation\n")
	if len(session.Recent) == 0 {
		b.WriteString("(no completed turns)\n")
		return b.String()
	}

	for _, turn := range session.Recent {
		turnTime := ""
		if !turn.CreatedAt.IsZero() {
			turnTime = " (" + turn.CreatedAt.Format(time.RFC3339) + ")"
		}
		b.WriteString("### User")
		b.WriteString(turnTime)
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(turn.User))
		b.WriteString("\n\n")
		b.WriteString("### Assistant")
		b.WriteString(turnTime)
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(turn.Assistant))
		b.WriteString("\n\n")
	}

	return b.String()
}

func renderSummaryMarkdown(mode ModeID, session SessionContext) string {
	var b strings.Builder

	timestamp := session.StartedAt.Format(time.RFC3339)
	if session.StartedAt.IsZero() {
		timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	b.WriteString("# Session Summary - ")
	b.WriteString(timestamp)
	b.WriteString("\n\n")
	b.WriteString("## Mode\n")
	b.WriteString(string(mode))
	b.WriteString("\n\n")
	b.WriteString("## Summary\n")
	if strings.TrimSpace(session.Summary) != "" {
		b.WriteString(strings.TrimSpace(session.Summary))
		b.WriteString("\n")
	} else {
		b.WriteString("(none)\n")
	}

	return b.String()
}

func extractSummaryBody(content string) string {
	parts := strings.Split(content, "## Summary\n")
	if len(parts) < 2 {
		return strings.TrimSpace(content)
	}

	return strings.TrimSpace(parts[len(parts)-1])
}

func extractTranscriptTurns(content string) []Turn {
	lines := strings.Split(content, "\n")
	var turns []Turn
	var current Turn
	var currentRole string

	flush := func() {
		if strings.TrimSpace(current.User) == "" && strings.TrimSpace(current.Assistant) == "" {
			current = Turn{}
			currentRole = ""
			return
		}
		turns = append(turns, Turn{
			User:      strings.TrimSpace(current.User),
			Assistant: strings.TrimSpace(current.Assistant),
		})
		current = Turn{}
		currentRole = ""
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "### User"):
			if strings.TrimSpace(current.User) != "" || strings.TrimSpace(current.Assistant) != "" {
				flush()
			}
			currentRole = "user"
		case strings.HasPrefix(line, "### Assistant"):
			currentRole = "assistant"
		case strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## "):
			continue
		default:
			switch currentRole {
			case "user":
				if strings.TrimSpace(current.User) != "" {
					current.User += "\n"
				}
				current.User += line
			case "assistant":
				if strings.TrimSpace(current.Assistant) != "" {
					current.Assistant += "\n"
				}
				current.Assistant += line
			}
		}
	}

	flush()
	return turns
}

func pickRelevantTurns(turns []Turn, query string, maxTurns int) []Turn {
	queryTerms := normalizedTerms(query)
	if len(queryTerms) == 0 {
		if len(turns) <= maxTurns {
			return turns
		}
		return turns[len(turns)-maxTurns:]
	}

	bestStart := -1
	bestScore := 0
	for i := range turns {
		end := i + maxTurns
		if end > len(turns) {
			end = len(turns)
		}
		score := 0
		for _, turn := range turns[i:end] {
			for _, term := range queryTerms {
				if strings.Contains(normalizeForMatching(turn.User), term) || strings.Contains(normalizeForMatching(turn.Assistant), term) {
					score++
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestStart = i
		}
	}

	if bestStart == -1 || bestScore == 0 {
		if len(turns) <= maxTurns {
			return turns
		}
		return turns[len(turns)-maxTurns:]
	}

	end := bestStart + maxTurns
	if end > len(turns) {
		end = len(turns)
	}
	return turns[bestStart:end]
}

func normalizedTerms(text string) []string {
	normalized := normalizeForMatching(text)
	seen := make(map[string]struct{})
	var terms []string
	for _, token := range strings.Fields(normalized) {
		if len(token) < 4 {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
	}
	return terms
}

func normalizeForMatching(text string) string {
	replacer := strings.NewReplacer(
		".", " ",
		",", " ",
		"!", " ",
		"?", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		"\n", " ",
		"\t", " ",
		"`", " ",
		"#", " ",
		"-", " ",
		"/", " ",
		"\"", " ",
		"'", " ",
	)

	return strings.ToLower(replacer.Replace(text))
}

func cosineSimilarity(a []float64, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}

	var dot float64
	var magA float64
	var magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}

	if magA == 0 || magB == 0 {
		return 0
	}

	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
