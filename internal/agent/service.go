package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"project-orb/internal/text"
)

type summarizer func(context.Context, *Client, string, []Turn) (string, error)

type Service struct {
	client    *Client
	mode      Mode
	summarize summarizer
	store     SessionStore
}

const (
	toolSearchMemories    = "search_memories"
	toolLoadMemoryExcerpt = "load_memory_excerpt"
)

type searchMemoriesArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type loadMemoryExcerptArgs struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	MaxTurns  int    `json:"max_turns"`
}

type memorySearchResult struct {
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	Excerpt   string `json:"excerpt,omitempty"`
	Score     int    `json:"score"`
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

func (s *Service) SetSessionStore(store SessionStore) {
	s.store = store
}

func (s *Service) PrepareSession(ctx context.Context, session *SessionContext) error {
	if session == nil {
		return fmt.Errorf("session cannot be nil")
	}

	session.EnsureMetadata()

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

func (s *Service) FinalizeSession(ctx context.Context, session *SessionContext) error {
	if session == nil {
		return fmt.Errorf("session cannot be nil")
	}

	session.EnsureMetadata()

	if len(session.Recent) == 0 {
		return nil
	}

	summary, err := s.summarize(ctx, s.client, session.Summary, session.Recent)
	if err != nil {
		return fmt.Errorf("finalize session: %w", err)
	}

	session.Summary = summary
	return nil
}

func (s *Service) LoadSession(_ context.Context) (SessionContext, bool, error) {
	if s.store == nil {
		return NewSessionContext(), false, nil
	}

	session, loaded, err := s.store.LoadLatestSummary(s.mode.ID)
	if err != nil {
		return SessionContext{}, false, fmt.Errorf("load session: %w", err)
	}

	session.EnsureMetadata()
	return session, loaded, nil
}

func (s *Service) StartupMessages(ctx context.Context, session SessionContext) ([]string, error) {
	switch s.mode.ID {
	case ModeCoach:
		return []string{text.CoachWelcomeMessage}, nil
	case ModePerformanceReview:
		return []string{text.PerformanceReviewWelcomeMessage}, nil
	case ModeAnalyst:
		// Return only the first message immediately for analyst mode
		// The second message will be loaded asynchronously
		return []string{text.AnalystWelcomeMessage}, nil
	default:
		return nil, nil
	}
}

func (s *Service) LoadAnalystSecondMessage(ctx context.Context, session SessionContext) (string, error) {
	if s.mode.ID != ModeAnalyst {
		return "", nil
	}

	if strings.TrimSpace(session.Summary) == "" {
		return text.AnalystFreshStartMessage, nil
	}

	guidance, err := s.generateStartupGuidance(ctx, session.Summary)
	if err != nil {
		return "", fmt.Errorf("generate startup guidance: %w", err)
	}
	if strings.TrimSpace(guidance) == "" {
		return text.AnalystReturningFallbackMessage, nil
	}

	return strings.TrimSpace(guidance), nil
}

func (s *Service) GenerateAnalysis(ctx context.Context, userMessage string, session SessionContext) (string, error) {
	systemMessage, err := s.mode.SystemMessage()
	if err != nil {
		return "", err
	}

	messages := buildConversationMessages(systemMessage, session, nil)
	messages = append(messages,
		chatMessage{Role: "user", Content: strings.TrimSpace(userMessage)},
		chatMessage{Role: "user", Content: analysisTaskPrompt},
	)

	analysis, err := s.client.CompleteWithTools(ctx, messages, s.analysisTools(userMessage, session))
	if err != nil {
		return "", fmt.Errorf("generate analysis: %w", err)
	}

	return strings.TrimSpace(analysis.Content), nil
}

func (s *Service) GenerateResponse(ctx context.Context, userMessage string, analysis string, session SessionContext) (<-chan string, <-chan error, error) {
	systemMessage, err := s.mode.SystemMessage()
	if err != nil {
		return nil, nil, err
	}

	messages := buildConversationMessages(systemMessage, session, nil)
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

func (s *Service) analysisTools(userMessage string, session SessionContext) []ToolHandler {
	if s.store == nil {
		return nil
	}

	var tools []ToolHandler
	if s.mode.AllowsTool(toolSearchMemories) {
		tools = append(tools, s.searchMemoriesTool(userMessage, session))
	}
	if s.mode.AllowsTool(toolLoadMemoryExcerpt) {
		tools = append(tools, s.loadMemoryExcerptTool(userMessage))
	}

	return tools
}

func (s *Service) searchMemoriesTool(userMessage string, session SessionContext) ToolHandler {
	return ToolHandler{
		Definition: chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        toolSearchMemories,
				Description: "Search prior saved analysis sessions for semantically relevant summaries.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "The memory search query. Use the user's request or the specific topic you want to recall.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of relevant sessions to return.",
						},
					},
				},
			},
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := searchMemoriesArgs{
				Query: userMessage,
				Limit: 3,
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return "", fmt.Errorf("decode search_memories args: %w", err)
				}
			}
			if strings.TrimSpace(args.Query) == "" {
				args.Query = userMessage
			}
			if args.Limit <= 0 || args.Limit > 5 {
				args.Limit = 3
			}

			memories, err := s.store.SearchRelevantSummaries(s.mode.ID, args.Query, session.Summary, args.Limit)
			if err != nil {
				return "", fmt.Errorf("search relevant summaries: %w", err)
			}

			results := make([]memorySearchResult, 0, len(memories))
			for _, memory := range memories {
				results = append(results, memorySearchResult(memory))
			}

			data, err := json.Marshal(map[string]any{
				"ok":      true,
				"query":   args.Query,
				"results": results,
			})
			if err != nil {
				return "", fmt.Errorf("encode search_memories result: %w", err)
			}
			return string(data), nil
		},
	}
}

func (s *Service) loadMemoryExcerptTool(userMessage string) ToolHandler {
	return ToolHandler{
		Definition: chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        toolLoadMemoryExcerpt,
				Description: "Load a short transcript excerpt from a previously found session when exact prior wording or details would help.",
				Parameters: map[string]any{
					"type": "object",
					"required": []string{
						"session_id",
					},
					"properties": map[string]any{
						"session_id": map[string]any{
							"type":        "string",
							"description": "The session id returned by search_memories.",
						},
						"query": map[string]any{
							"type":        "string",
							"description": "What details to look for inside the transcript.",
						},
						"max_turns": map[string]any{
							"type":        "integer",
							"description": "Maximum number of conversation turns to include.",
						},
					},
				},
			},
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args loadMemoryExcerptArgs
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("decode load_memory_excerpt args: %w", err)
			}
			if strings.TrimSpace(args.SessionID) == "" {
				return "", fmt.Errorf(text.SessionIDRequired)
			}
			if strings.TrimSpace(args.Query) == "" {
				args.Query = userMessage
			}
			if args.MaxTurns <= 0 || args.MaxTurns > 3 {
				args.MaxTurns = 1
			}

			excerpt, err := s.store.LoadTranscriptExcerpt(s.mode.ID, args.SessionID, args.Query, args.MaxTurns)
			if err != nil {
				return "", fmt.Errorf("load transcript excerpt: %w", err)
			}

			data, err := json.Marshal(map[string]any{
				"ok":         true,
				"session_id": args.SessionID,
				"query":      args.Query,
				"excerpt":    excerpt,
			})
			if err != nil {
				return "", fmt.Errorf("encode load_memory_excerpt result: %w", err)
			}
			return string(data), nil
		},
	}
}

func (s *Service) generateStartupGuidance(ctx context.Context, summary string) (string, error) {
	systemMessage, err := s.mode.SystemMessage()
	if err != nil {
		return "", err
	}

	agentName := ExtractAgentName(systemMessage)
	if strings.TrimSpace(agentName) == "" {
		agentName = text.DefaultAssistantName
	}

	messages := []chatMessage{
		{Role: "system", Content: systemMessage},
		{
			Role:    "user",
			Content: text.StartupGuidanceRoleMapping(agentName),
		},
		{Role: "user", Content: text.ConversationSummary(summary)},
		{Role: "user", Content: startupTaskPrompt},
	}

	guidance, err := s.client.Complete(ctx, messages)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(guidance), nil
}
