package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type e2eLLMTransport struct {
	t          *testing.T
	scenario   string
	requests   []chatRequest
	streamText string
}

func (tr *e2eLLMTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	var payload chatRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		tr.t.Fatalf("decode request: %v", err)
	}
	tr.requests = append(tr.requests, payload)

	if payload.Stream {
		switch tr.scenario {
		case "memory-recall":
			if !containsMessage(payload.Messages, buildResponseContext("The user is asking for recall. We found a prior session about work avoidance and fear of judgment, plus an excerpt about being judged when shipping.")) {
				tr.t.Fatalf("expected final response request to include analysis context, got %#v", payload.Messages)
			}
		}
		return tr.streamResponse(tr.streamText), nil
	}

	switch tr.scenario {
	case "memory-recall":
		return tr.memoryRecallResponse(payload), nil
	case "raw-transcript-search":
		return tr.rawTranscriptSearchResponse(payload), nil
	case "raw-latest-transcript":
		return tr.rawLatestTranscriptResponse(payload), nil
	case "no-tool":
		return tr.noToolResponse(payload), nil
	case "no-memory-hit":
		return tr.noMemoryHitResponse(payload), nil
	case "empty-excerpt":
		return tr.emptyExcerptResponse(payload), nil
	case "coach-no-memory-tools":
		return tr.coachNoMemoryToolsResponse(payload), nil
	case "runaway-tool-loop":
		return tr.runawayToolLoopResponse(payload), nil
	default:
		tr.t.Fatalf("unknown e2e scenario %q", tr.scenario)
		return nil, nil
	}
}

func (tr *e2eLLMTransport) memoryRecallResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		if len(payload.Tools) != 4 {
			tr.t.Fatalf("expected 4 analysis tools, got %d", len(payload.Tools))
		}
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-search",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolSearchMemories,
							Arguments: `{"query":"have we talked about this before?","limit":1}`,
						},
					}},
				},
			}},
		})
	case 2:
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role != "tool" || last.Name != toolSearchMemories {
			tr.t.Fatalf("expected search_memories tool result in second request, got %#v", last)
		}
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-excerpt",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolLoadMemoryExcerpt,
							Arguments: `{"session_id":"2026-04-05-120000","query":"what did we say before?","max_turns":1}`,
						},
					}},
				},
			}},
		})
	case 3:
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role != "tool" || last.Name != toolLoadMemoryExcerpt {
			tr.t.Fatalf("expected load_memory_excerpt tool result in third request, got %#v", last)
		}
		tr.streamText = "Yes. We talked about this before in a session about work avoidance and fear of being judged when shipping."
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "The user is asking for recall. We found a prior session about work avoidance and fear of judgment, plus an excerpt about being judged when shipping.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for memory recall scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) rawTranscriptSearchResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		if len(payload.Tools) != 4 {
			tr.t.Fatalf("expected 4 analysis tools, got %d", len(payload.Tools))
		}
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-search-transcripts",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolSearchMemoryTranscripts,
							Arguments: `{"query":"stop-doing list","limit":1,"max_turns":1}`,
						},
					}},
				},
			}},
		})
	case 2:
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role != "tool" || last.Name != toolSearchMemoryTranscripts {
			tr.t.Fatalf("expected search_memory_transcripts tool result in second request, got %#v", last)
		}
		tr.streamText = "Yes. We did discuss a stop-doing list before, and I found it in a saved transcript rather than only in the summary."
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "The user is challenging the provenance of a remembered idea. I searched raw transcripts and found a matching excerpt mentioning a stop-doing list.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for raw transcript search scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) rawLatestTranscriptResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		if len(payload.Tools) != 4 {
			tr.t.Fatalf("expected 4 analysis tools, got %d", len(payload.Tools))
		}
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-transcript",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolLoadMemoryTranscript,
							Arguments: `{}`,
						},
					}},
				},
			}},
		})
	case 2:
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role != "tool" || last.Name != toolLoadMemoryTranscript {
			tr.t.Fatalf("expected load_memory_transcript tool result in second request, got %#v", last)
		}
		tr.streamText = "The latest saved session included: \"I want the raw text from last time.\""
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "The user asked for exact prior wording. I loaded the latest saved transcript and can quote from it directly.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for raw latest transcript scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) noToolResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		if len(payload.Tools) != 4 {
			tr.t.Fatalf("expected analysis tools to be available in analysis mode, got %d", len(payload.Tools))
		}
		tr.streamText = "Let's stay with what feels heavy about it right now."
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "The user is describing a present-moment feeling and does not need cross-session recall.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for no-tool scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) coachNoMemoryToolsResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		if len(payload.Tools) != 0 {
			tr.t.Fatalf("expected no memory tools for coach mode, got %d", len(payload.Tools))
		}
		tr.streamText = "I can help think it through, but in this mode I am not recalling prior analysis sessions."
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "The user is asking for recall, but no cross-session memory tools are available in this mode.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for coach scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) noMemoryHitResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		if len(payload.Tools) != 4 {
			tr.t.Fatalf("expected analysis tools to be available in analysis mode, got %d", len(payload.Tools))
		}
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-search",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolSearchMemories,
							Arguments: `{"query":"have we talked about this before?","limit":1}`,
						},
					}},
				},
			}},
		})
	case 2:
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role != "tool" || last.Name != toolSearchMemories {
			tr.t.Fatalf("expected search_memories tool result in second request, got %#v", last)
		}
		tr.streamText = "I do not see a clearly related prior session, so let's stay with what is happening now."
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "The memory search returned no clear matches, so I should not claim that we discussed this before.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for no-memory-hit scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) emptyExcerptResponse(payload chatRequest) *http.Response {
	switch len(tr.requests) {
	case 1:
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-search",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolSearchMemories,
							Arguments: `{"query":"what exactly did we say last time?","limit":1}`,
						},
					}},
				},
			}},
		})
	case 2:
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:   "call-excerpt",
						Type: "function",
						Function: chatToolCallFunction{
							Name:      toolLoadMemoryExcerpt,
							Arguments: `{"session_id":"2026-04-05-120000","query":"what exactly did we say last time?","max_turns":1}`,
						},
					}},
				},
			}},
		})
	case 3:
		tr.streamText = "I found a related prior session, but not a precise excerpt for that detail."
		return tr.jsonResponse(completionResponse{
			Choices: []completionChoice{{
				Message: chatMessage{
					Role:    "assistant",
					Content: "There is a related prior session, but the excerpt came back empty, so I should avoid pretending to know the exact wording.",
				},
			}},
		})
	default:
		tr.t.Fatalf("unexpected request count for empty-excerpt scenario: %d", len(tr.requests))
		return nil
	}
}

func (tr *e2eLLMTransport) runawayToolLoopResponse(payload chatRequest) *http.Response {
	return tr.jsonResponse(completionResponse{
		Choices: []completionChoice{{
			Message: chatMessage{
				Role: "assistant",
				ToolCalls: []chatToolCall{{
					ID:   "call-loop",
					Type: "function",
					Function: chatToolCallFunction{
						Name:      toolSearchMemories,
						Arguments: `{"query":"loop forever","limit":1}`,
					},
				}},
			},
		}},
	})
}

func (tr *e2eLLMTransport) jsonResponse(response completionResponse) *http.Response {
	data, _ := json.Marshal(response)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     make(http.Header),
	}
}

func (tr *e2eLLMTransport) streamResponse(text string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for _, token := range strings.Split(text, " ") {
			data, _ := json.Marshal(completionResponse{
				Choices: []completionChoice{{
					Delta: chatMessage{Content: token + " "},
				}},
			})
			_, _ = pw.Write([]byte("data: "))
			_, _ = pw.Write(data)
			_, _ = pw.Write([]byte("\n\n"))
		}
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()
	return &http.Response{
		StatusCode: 200,
		Body:       pr,
		Header:     make(http.Header),
	}
}

func containsMessage(messages []chatMessage, content string) bool {
	for _, msg := range messages {
		if msg.Content == content {
			return true
		}
	}
	return false
}

func newE2EClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		CompletionsURL: "http://localhost:8080/v1/chat/completions",
		Model:          "test-model",
		HTTPClient:     &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func readRunnerOutput(t *testing.T, channels StreamChannels) string {
	t.Helper()
	var output strings.Builder

	for channels.TokenCh != nil || channels.ErrCh != nil || channels.DoneCh != nil {
		select {
		case token, ok := <-channels.TokenCh:
			if !ok {
				channels.TokenCh = nil
				continue
			}
			output.WriteString(token)
		case err, ok := <-channels.ErrCh:
			if !ok {
				channels.ErrCh = nil
				continue
			}
			t.Fatalf("runner returned error: %v", err)
		case result, ok := <-channels.DoneCh:
			if !ok {
				channels.DoneCh = nil
				continue
			}
			if result.Canceled {
				t.Fatal("runner unexpectedly canceled")
			}
			channels.DoneCh = nil
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for runner output")
		}
	}

	return strings.TrimSpace(output.String())
}

func TestRunnerE2EAnalysisMemoryRecall(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "memory-recall"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nWork avoidance and fear of judgment.", Score: 9},
		},
		transcriptExcerpt: "User: I was afraid of being judged when shipping.\nAssistant: We named the fear directly.",
	}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("have we talked about this before?", NewSessionContext()))

	if !strings.Contains(output, "fear of being judged") {
		t.Fatalf("expected final output to include recalled detail, got %q", output)
	}
	if store.transcriptCalls != 1 {
		t.Fatalf("expected transcript excerpt tool to be used once, got %d", store.transcriptCalls)
	}
}

func TestRunnerE2EAnalysisCanSkipMemoryTools(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "no-tool"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nWork avoidance and fear of judgment.", Score: 9},
		},
		transcriptExcerpt: "User: I was afraid of being judged when shipping.\nAssistant: We named the fear directly.",
	}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("I feel heavy about this today.", NewSessionContext()))

	if !strings.Contains(output, "right now") {
		t.Fatalf("expected present-focused output, got %q", output)
	}
	if store.transcriptCalls != 0 {
		t.Fatalf("expected no transcript lookup, got %d", store.transcriptCalls)
	}
}

func TestRunnerE2ECoachDoesNotExposeAnalysisMemoryTools(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "coach-no-memory-tools"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nWork avoidance and fear of judgment.", Score: 9},
		},
		transcriptExcerpt: "User: I was afraid of being judged when shipping.\nAssistant: We named the fear directly.",
	}

	service, err := NewService(client, DefaultMode())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("have we talked about this before?", NewSessionContext()))

	if !strings.Contains(output, "not recalling prior analysis sessions") {
		t.Fatalf("expected coach output without memory recall, got %q", output)
	}
	if store.transcriptCalls != 0 {
		t.Fatalf("expected no transcript lookup in coach mode, got %d", store.transcriptCalls)
	}
}

func TestRunnerE2EAnalysisNoMemoryHit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "no-memory-hit"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("have we talked about this before?", NewSessionContext()))

	if !strings.Contains(output, "do not see a clearly related prior session") {
		t.Fatalf("expected no-hit output, got %q", output)
	}
	if store.transcriptCalls != 0 {
		t.Fatalf("expected no transcript lookup when search has no hit, got %d", store.transcriptCalls)
	}
}

func TestRunnerE2EAnalysisEmptyExcerptFallsBackGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "empty-excerpt"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nA related prior session exists.", Score: 9},
		},
		transcriptExcerpt: "",
	}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("what exactly did we say last time?", NewSessionContext()))

	if !strings.Contains(output, "not a precise excerpt") {
		t.Fatalf("expected graceful empty-excerpt fallback, got %q", output)
	}
	if store.transcriptCalls != 1 {
		t.Fatalf("expected one transcript lookup, got %d", store.transcriptCalls)
	}
}

func TestRunnerE2EAnalysisCanLoadLatestRawTranscript(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "raw-latest-transcript"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		transcriptExact: MemoryTranscript{
			SessionID:  "2026-04-05-120000",
			Transcript: "# Session - 2026-04-05T12:00:00Z\n\n## Conversation\n### User\nI want the raw text from last time.\n\n### Assistant\nHere is what we said.",
		},
	}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("tell me the raw text from our last session", NewSessionContext()))

	if !strings.Contains(output, "raw text from last time") {
		t.Fatalf("expected final output to include quoted transcript text, got %q", output)
	}
	if store.exactTranscriptCalls < 1 {
		t.Fatalf("expected at least one exact transcript lookup, got %d", store.exactTranscriptCalls)
	}
}

func TestRunnerE2EAnalysisCanSearchRawTranscripts(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "raw-transcript-search"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		transcriptMatches: []MemorySnippet{{
			SessionID: "2026-04-05-120000",
			Excerpt:   "User: What should go on the stop-doing list?\nAssistant: Let's create a stop-doing list for the habits that keep derailing you.",
			Score:     8,
		}},
	}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	runner := Runner{Service: service}
	output := readRunnerOutput(t, runner.Start("what is that stop-doing list coming from? i dont remember talking about it in previous sessions", NewSessionContext()))

	if !strings.Contains(output, "stop-doing list") {
		t.Fatalf("expected final output to reference transcript hit, got %q", output)
	}
}

func TestGenerateAnalysisStopsRunawayToolLoop(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if _, err := EnsurePersonaFile(); err != nil {
		t.Fatalf("EnsurePersonaFile() error = %v", err)
	}

	transport := &e2eLLMTransport{t: t, scenario: "runaway-tool-loop"}
	client := newE2EClient(t, transport)
	store := &stubSessionStore{
		memories: []MemorySnippet{
			{SessionID: "2026-04-05-120000", Summary: "## Overview\nLoop test", Score: 9},
		},
	}

	service, err := NewService(client, BuiltInModes()[2])
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetSessionStore(store)

	_, err = service.GenerateAnalysis(context.Background(), "loop forever", NewSessionContext())
	if err == nil {
		t.Fatal("expected runaway tool loop to return an error")
	}
	if !strings.Contains(err.Error(), "tool calling exceeded maximum iterations") {
		t.Fatalf("expected max-iteration error, got %v", err)
	}
}
