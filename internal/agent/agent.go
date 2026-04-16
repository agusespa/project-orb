package agent

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"project-orb/internal/text"
)

const (
	maxWorkingHistoryWords = 3000 // Recent history before compaction
	minWorkingHistoryTurns = 3    // Always keep at least this many turns
	maxTotalWords          = 6000 // Total context limit (recent + summary) for UI indicator
)

//go:embed prompts/analysis_task.md
var analysisTaskPrompt string

//go:embed prompts/summary_task.md
var summaryTaskPrompt string

//go:embed prompts/startup_task.md
var startupTaskPrompt string

type Turn struct {
	User      string
	Assistant string
	CreatedAt time.Time
}

type SessionContext struct {
	SessionID      string
	StartedAt      time.Time
	Summary        string
	WorkingHistory []Turn
	RawHistory     []Turn
}

type MemorySnippet struct {
	SessionID string
	Summary   string
	Excerpt   string
	Score     int
}

type MemoryTranscript struct {
	SessionID  string
	Transcript string
}

func NewSessionContext() SessionContext {
	now := time.Now().UTC()
	return SessionContext{
		SessionID: now.Format("2006-01-02-150405"),
		StartedAt: now,
	}
}

func (s *SessionContext) EnsureMetadata() {
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now().UTC()
	}
	if strings.TrimSpace(s.SessionID) == "" {
		s.SessionID = s.StartedAt.Format("2006-01-02-150405")
	}
}

func (s *SessionContext) AddTurn(turn Turn) {
	s.EnsureMetadata()
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now().UTC()
	}
	s.WorkingHistory = append(s.WorkingHistory, turn)
	s.RawHistory = append(s.RawHistory, turn)
}

func (s *SessionContext) WordCount() int {
	count := 0
	for _, turn := range s.WorkingHistory {
		count += countWords(turn.User)
		count += countWords(turn.Assistant)
	}
	return count
}

func (s *SessionContext) TotalWordCount() int {
	count := s.WordCount()
	count += countWords(s.Summary)
	return count
}

func (s *SessionContext) ContextUsagePercent() float64 {
	total := s.TotalWordCount()
	return (float64(total) / float64(maxTotalWords)) * 100
}

func countWords(s string) int {
	return len(strings.Fields(s))
}

func MaxTotalWords() int {
	return maxTotalWords
}

func buildResponseContext(analysis string) string {
	return text.AnalysisContext(analysis)
}

func buildConversationMessages(systemMessage string, session SessionContext, memories []MemorySnippet) []chatMessage {
	messages := []chatMessage{
		{Role: "system", Content: systemMessage},
	}

	if memoryContext := buildMemoryContext(memories); memoryContext != "" {
		messages = append(messages, chatMessage{
			Role:    "user",
			Content: memoryContext,
		})
	}

	if summary := strings.TrimSpace(session.Summary); summary != "" {
		messages = append(messages, chatMessage{
			Role:    "user",
			Content: text.ConversationSummary(summary),
		})
	}

	for _, turn := range session.WorkingHistory {
		if user := strings.TrimSpace(turn.User); user != "" {
			messages = append(messages, chatMessage{Role: "user", Content: user})
		}
		if assistant := strings.TrimSpace(turn.Assistant); assistant != "" {
			messages = append(messages, chatMessage{Role: "assistant", Content: assistant})
		}
	}

	return messages
}

func buildMemoryContext(memories []MemorySnippet) string {
	if len(memories) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(text.RelevantPastSessionSummaries())
	for _, memory := range memories {
		summary := strings.TrimSpace(memory.Summary)
		excerpt := strings.TrimSpace(memory.Excerpt)
		if summary == "" && excerpt == "" {
			continue
		}

		b.WriteString("\n")
		b.WriteString(text.SessionSummaryHeading(memory.SessionID))
		if summary != "" {
			b.WriteString(summary)
			b.WriteString("\n")
		}
		if excerpt != "" {
			b.WriteString(text.SupportingTranscriptExcerptHeading())
			b.WriteString(excerpt)
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func processSummaryWithPrompt(ctx context.Context, client *Client, existingSummary string, turns []Turn, prompt string) (string, error) {
	if len(turns) == 0 && strings.TrimSpace(existingSummary) == "" {
		return existingSummary, nil
	}

	var messages []chatMessage

	if summary := strings.TrimSpace(existingSummary); summary != "" {
		messages = append(messages, chatMessage{
			Role:    "user",
			Content: text.ExistingConversationSummary(summary),
		})
	}

	for _, turn := range turns {
		if user := strings.TrimSpace(turn.User); user != "" {
			messages = append(messages, chatMessage{Role: "user", Content: user})
		}
		if assistant := strings.TrimSpace(turn.Assistant); assistant != "" {
			messages = append(messages, chatMessage{Role: "assistant", Content: assistant})
		}
	}

	messages = append(messages, chatMessage{Role: "user", Content: prompt})

	summary, err := client.Complete(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("process summary: %w", err)
	}

	return strings.TrimSpace(summary), nil
}

func CompactContext(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
	return processSummaryWithPrompt(ctx, client, existingSummary, turns, summaryTaskPrompt)
}

func EvaluatePerformanceReview(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
	return processSummaryWithPrompt(ctx, client, existingSummary, turns, performanceReviewSummaryTaskPrompt)
}
