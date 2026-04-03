package coach

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	requestTimeout = 10 * time.Minute
	maxRecentTurns = 6
)

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

	systemMessage, err := LoadSystemMessage()
	if err != nil {
		return "", err
	}

	messages := []chatMessage{
		{Role: "system", Content: systemMessage},
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

const analysisTaskPrompt = `Analyze the situation before responding.

Identify:

- what the user is really asking
- possible emotional context
- potential cognitive biases
- useful coaching direction

Return a short structured analysis.`

const summaryTaskPrompt = `Update the rolling conversation summary using the prior summary and the conversation turns above.

Keep it compact and useful for future coaching.

Capture:

- the user's main situation or goals
- emotionally relevant context
- important patterns, tensions, or recurring themes
- any concrete decisions, commitments, or open questions

Do not include filler.
Return a short structured summary.`

const responseTaskPrompt = `Provide a concise, thoughtful coaching response.

Guidelines:

- natural tone
- not robotic
- may ask one or two thoughtful questions
- prefer clarity over length
- help the conversation continue naturally instead of closing it too early
- when appropriate, end with a reflective question or gentle invitation to continue`
