package agent

import (
	"context"
	"fmt"
	"strings"
)

type summarizer func(context.Context, *Client, string, []Turn) (string, error)

type Service struct {
	client    *Client
	mode      Mode
	summarize summarizer
}

func NewService(client *Client, mode Mode) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("client cannot be nil")
	}

	return &Service{
		client:    client,
		mode:      mode,
		summarize: updateConversationSummary,
	}, nil
}

func newServiceWithSummarizer(client *Client, mode Mode, summarize summarizer) *Service {
	return &Service{
		client:    client,
		mode:      mode,
		summarize: summarize,
	}
}

func (s *Service) PrepareSession(ctx context.Context, session *SessionContext) error {
	if session == nil {
		return fmt.Errorf("session cannot be nil")
	}

	// Don't compact if under word limit or too few turns
	if session.WordCount() <= maxRecentWords || len(session.Recent) <= minRecentTurns {
		return nil
	}

	// Compact oldest turns until we're under the word limit
	overflowTurns := []Turn{}
	for session.WordCount() > maxRecentWords && len(session.Recent) > minRecentTurns {
		overflowTurns = append(overflowTurns, session.Recent[0])
		session.Recent = session.Recent[1:]
	}

	if len(overflowTurns) == 0 {
		return nil
	}

	summary, err := s.summarize(ctx, s.client, session.Summary, overflowTurns)
	if err != nil {
		return err
	}

	session.Summary = summary
	return nil
}

func (s *Service) GenerateAnalysis(ctx context.Context, userMessage string, session SessionContext) (string, error) {
	systemMessage, err := s.mode.SystemMessage()
	if err != nil {
		return "", err
	}

	messages := buildConversationMessages(systemMessage, session)
	messages = append(messages,
		chatMessage{Role: "user", Content: strings.TrimSpace(userMessage)},
		chatMessage{Role: "user", Content: analysisTaskPrompt},
	)

	analysis, err := s.client.Complete(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("generate analysis: %w", err)
	}

	return strings.TrimSpace(analysis), nil
}

func (s *Service) GenerateResponse(ctx context.Context, userMessage string, analysis string, session SessionContext) (<-chan string, <-chan error, error) {
	systemMessage, err := s.mode.SystemMessage()
	if err != nil {
		return nil, nil, err
	}

	messages := buildConversationMessages(systemMessage, session)
	messages = append(messages,
		chatMessage{Role: "user", Content: buildResponseContext(analysis)},
		chatMessage{Role: "user", Content: strings.TrimSpace(userMessage)},
	)

	tokenCh, errCh, err := s.client.StreamMessages(ctx, messages)
	if err != nil {
		return nil, nil, fmt.Errorf("generate response: %w", err)
	}

	return tokenCh, errCh, nil
}
