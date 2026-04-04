package agent

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"
)

const (
	requestTimeout = 10 * time.Minute
	maxRecentTurns = 6
)

//go:embed prompts/summary_system.md
var summarySystemPrompt string

//go:embed prompts/analysis_task.md
var analysisTaskPrompt string

//go:embed prompts/summary_task.md
var summaryTaskPrompt string

//go:embed prompts/response_task.md
var responseTaskPrompt string

//go:embed prompts/welcome_task.md
var welcomeTaskPrompt string

type Turn struct {
	User      string
	Assistant string
}

type SessionContext struct {
	Summary string
	Recent  []Turn
}

func (s *SessionContext) AddTurn(turn Turn) {
	s.Recent = append(s.Recent, turn)
}

func buildResponseContext(analysis string) string {
	return "Coaching analysis:\n" + strings.TrimSpace(analysis)
}

func buildConversationMessages(systemMessage string, session SessionContext) []chatMessage {
	messages := []chatMessage{
		{Role: "system", Content: systemMessage},
	}

	if summary := strings.TrimSpace(session.Summary); summary != "" {
		messages = append(messages, chatMessage{
			Role:    "user",
			Content: "Conversation summary:\n" + summary,
		})
	}

	for _, turn := range session.Recent {
		if user := strings.TrimSpace(turn.User); user != "" {
			messages = append(messages, chatMessage{Role: "user", Content: user})
		}
		if assistant := strings.TrimSpace(turn.Assistant); assistant != "" {
			messages = append(messages, chatMessage{Role: "assistant", Content: assistant})
		}
	}

	return messages
}

func updateConversationSummary(ctx context.Context, client *Client, existingSummary string, turns []Turn) (string, error) {
	if len(turns) == 0 {
		return existingSummary, nil
	}

	messages := []chatMessage{
		{Role: "system", Content: summarySystemPrompt},
	}

	if summary := strings.TrimSpace(existingSummary); summary != "" {
		messages = append(messages, chatMessage{
			Role:    "user",
			Content: "Existing conversation summary:\n" + summary,
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

	messages = append(messages, chatMessage{Role: "user", Content: summaryTaskPrompt})

	summary, err := client.Complete(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("update conversation summary: %w", err)
	}

	return strings.TrimSpace(summary), nil
}
