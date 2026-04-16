package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"project-orb/internal/text"
)

type contextCompactor func(context.Context, *Client, string, []Turn) (string, error)

type Service struct {
	client         *Client
	mode           Mode
	compactContext contextCompactor
	store          SessionStore
}

const (
	toolSearchMemories          = "search_memories"
	toolSearchMemoryTranscripts = "search_memory_transcripts"
	toolLoadMemoryExcerpt       = "load_memory_excerpt"
	toolLoadMemoryTranscript    = "load_memory_transcript"
)

type searchMemoriesArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type searchTranscriptArgs struct {
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
	MaxTurns int    `json:"max_turns"`
}

type loadMemoryExcerptArgs struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	MaxTurns  int    `json:"max_turns"`
}

type loadMemoryTranscriptArgs struct {
	SessionID string `json:"session_id"`
}

type memoryIntent struct {
	priorRecall   bool
	latestSession bool
	rawTranscript bool
	provenance    bool
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
		client:         client,
		mode:           mode,
		compactContext: CompactContext,
	}, nil
}

func newServiceWithCompactor(client *Client, mode Mode, compactor contextCompactor) *Service {
	return &Service{
		client:         client,
		mode:           mode,
		compactContext: compactor,
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

	needsHistoryCompaction := session.WordCount() > maxWorkingHistoryWords
	needsSummaryCompaction := session.TotalWordCount() > maxTotalWords
	if !needsHistoryCompaction && !needsSummaryCompaction {
		return nil
	}

	overflowTurns := []Turn{}
	if needsHistoryCompaction {
		// Compact oldest turns until we're under the working-history limit.
		for session.WordCount() > maxWorkingHistoryWords && len(session.WorkingHistory) > minWorkingHistoryTurns {
			overflowTurns = append(overflowTurns, session.WorkingHistory[0])
			session.WorkingHistory = session.WorkingHistory[1:]
		}
	}

	// If only the summary is oversized, re-compact the summary in place.
	if len(overflowTurns) == 0 && strings.TrimSpace(session.Summary) == "" {
		return nil
	}

	summary, err := s.compactContext(ctx, s.client, session.Summary, overflowTurns)
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

	if len(session.WorkingHistory) == 0 {
		return nil
	}

	if s.mode.Finalizer == nil {
		return nil
	}

	summary, err := s.mode.Finalizer(ctx, s.client, session.Summary, session.WorkingHistory)
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
	case ModeAnalysis:
		// Return only the first message immediately for analysis mode
		// The second message will be loaded asynchronously
		return []string{text.AnalysisWelcomeMessage}, nil
	default:
		return nil, nil
	}
}

func (s *Service) LoadAnalysisSecondMessage(ctx context.Context, session SessionContext) (string, error) {
	if s.mode.ID != ModeAnalysis {
		return "", nil
	}

	if strings.TrimSpace(session.Summary) == "" {
		return text.AnalysisFreshStartMessage, nil
	}

	guidance, err := s.generateStartupGuidance(ctx, session.Summary)
	if err != nil {
		return "", fmt.Errorf("generate startup guidance: %w", err)
	}
	if strings.TrimSpace(guidance) == "" {
		return text.AnalysisReturningFallbackMessage, nil
	}

	return strings.TrimSpace(guidance), nil
}

func (s *Service) GenerateAnalysis(ctx context.Context, userMessage string, session SessionContext) (string, error) {
	systemMessage, err := s.mode.SystemMessage()
	if err != nil {
		return "", err
	}

	messages := buildConversationMessages(systemMessage, session, nil)
	prefetched, err := s.prefetchMemoryMessages(ctx, userMessage, session)
	if err != nil {
		return "", err
	}
	messages = append(messages, prefetched...)
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

func (s *Service) prefetchMemoryMessages(ctx context.Context, userMessage string, session SessionContext) ([]chatMessage, error) {
	if s.store == nil || s.mode.ID != ModeAnalysis {
		return nil, nil
	}

	intent := detectMemoryIntent(userMessage)
	var messages []chatMessage

	if intent.latestSession || intent.rawTranscript {
		transcript, err := s.store.LoadTranscript(s.mode.ID, "")
		if err != nil {
			return nil, fmt.Errorf("prefetch latest transcript: %w", err)
		}
		if strings.TrimSpace(transcript.Transcript) != "" {
			messages = append(messages, chatMessage{
				Role:    "user",
				Content: renderVerifiedLatestTranscript(transcript),
			})
		} else {
			messages = append(messages, chatMessage{
				Role:    "user",
				Content: "Verified memory lookup: no saved raw transcript was available for the latest session.",
			})
		}
	}

	if intent.priorRecall || intent.provenance {
		var memories []MemorySnippet

		transcriptHits, err := s.store.SearchTranscriptExcerpts(s.mode.ID, userMessage, 2, 1)
		if err != nil {
			return nil, fmt.Errorf("prefetch transcript matches: %w", err)
		}
		memories = append(memories, transcriptHits...)

		summaryHits, err := s.store.SearchRelevantSummaries(s.mode.ID, userMessage, session.Summary, 2)
		if err != nil {
			return nil, fmt.Errorf("prefetch summary matches: %w", err)
		}
		memories = append(memories, summaryHits...)

		if memoryContext := buildMemoryContext(memories); memoryContext != "" {
			messages = append(messages, chatMessage{
				Role:    "user",
				Content: "Verified memory evidence:\n" + memoryContext,
			})
		} else {
			messages = append(messages, chatMessage{
				Role:    "user",
				Content: "Verified memory lookup: no matching saved memory evidence was found for this request.",
			})
		}
	}

	return messages, nil
}

func detectMemoryIntent(userMessage string) memoryIntent {
	normalized := normalizeForMatching(userMessage)
	intent := memoryIntent{}

	if containsAny(normalized,
		"last time",
		"last session",
		"previous session",
		"previous conversation",
		"what did we talk about last",
		"what did we talk about last time",
	) {
		intent.latestSession = true
		intent.priorRecall = true
	}

	if containsAny(normalized,
		"raw session",
		"raw file",
		"raw transcript",
		"transcript",
		"exact wording",
		"exact words",
		"verbatim",
	) {
		intent.rawTranscript = true
		intent.priorRecall = true
	}

	if containsAny(normalized,
		"where did",
		"coming from",
		"are you sure",
		"dont remember talking about",
		"do not remember talking about",
		"remember talking about",
		"did we talk about",
		"talked about this before",
		"previous sessions",
	) {
		intent.provenance = true
		intent.priorRecall = true
	}

	if containsAny(normalized,
		"have we talked about this before",
		"did we discuss this before",
		"did we talk about this before",
	) {
		intent.priorRecall = true
	}

	return intent
}

func containsAny(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func renderVerifiedLatestTranscript(transcript MemoryTranscript) string {
	var b strings.Builder
	b.WriteString("Verified latest saved session transcript")
	if strings.TrimSpace(transcript.SessionID) != "" {
		b.WriteString(" (session ")
		b.WriteString(strings.TrimSpace(transcript.SessionID))
		b.WriteString(")")
	}
	b.WriteString(":\n")
	b.WriteString(strings.TrimSpace(transcript.Transcript))
	return b.String()
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
	if s.mode.AllowsTool(toolSearchMemoryTranscripts) {
		tools = append(tools, s.searchTranscriptMemoriesTool(userMessage))
	}
	if s.mode.AllowsTool(toolLoadMemoryExcerpt) {
		tools = append(tools, s.loadMemoryExcerptTool(userMessage))
	}
	if s.mode.AllowsTool(toolLoadMemoryTranscript) {
		tools = append(tools, s.loadMemoryTranscriptTool())
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

func (s *Service) searchTranscriptMemoriesTool(userMessage string) ToolHandler {
	return ToolHandler{
		Definition: chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        toolSearchMemoryTranscripts,
				Description: "Search saved raw session transcripts directly when the user asks where something came from, whether it was discussed before, or for details that may not appear in summaries.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "The phrase, topic, or detail to search for across saved raw transcripts.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of matching sessions to return.",
						},
						"max_turns": map[string]any{
							"type":        "integer",
							"description": "Maximum number of turns to include per matching session.",
						},
					},
				},
			},
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := searchTranscriptArgs{
				Query:    userMessage,
				Limit:    3,
				MaxTurns: 1,
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return "", fmt.Errorf("decode search_memory_transcripts args: %w", err)
				}
			}
			if strings.TrimSpace(args.Query) == "" {
				args.Query = userMessage
			}
			if args.Limit <= 0 || args.Limit > 5 {
				args.Limit = 3
			}
			if args.MaxTurns <= 0 || args.MaxTurns > 3 {
				args.MaxTurns = 1
			}

			memories, err := s.store.SearchTranscriptExcerpts(s.mode.ID, args.Query, args.Limit, args.MaxTurns)
			if err != nil {
				return "", fmt.Errorf("search transcript excerpts: %w", err)
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
				return "", fmt.Errorf("encode search_memory_transcripts result: %w", err)
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

func (s *Service) loadMemoryTranscriptTool() ToolHandler {
	return ToolHandler{
		Definition: chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        toolLoadMemoryTranscript,
				Description: "Load the exact saved transcript markdown for a prior analysis session. Omit session_id to load the latest saved session. Use this when the user asks for raw text, exact wording, a quote, or what happened in the last session.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"session_id": map[string]any{
							"type":        "string",
							"description": "Optional specific session id. Leave empty to load the latest saved analysis session transcript.",
						},
					},
				},
			},
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args loadMemoryTranscriptArgs
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return "", fmt.Errorf("decode load_memory_transcript args: %w", err)
				}
			}

			transcript, err := s.store.LoadTranscript(s.mode.ID, args.SessionID)
			if err != nil {
				return "", fmt.Errorf("load transcript: %w", err)
			}

			data, err := json.Marshal(map[string]any{
				"ok":         true,
				"found":      strings.TrimSpace(transcript.Transcript) != "",
				"session_id": transcript.SessionID,
				"transcript": transcript.Transcript,
			})
			if err != nil {
				return "", fmt.Errorf("encode load_memory_transcript result: %w", err)
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
