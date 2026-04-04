# Analysis Mode Memory System

## Overview

Analysis mode provides a sophisticated memory system that enables continuous performance tracking and contextual feedback across sessions. The system persists session data, generates summaries, and uses vector search to provide relevant historical context during conversations.

## Session Lifecycle

### Session Start
1. Agent checks for previous session summary
2. If found, agent reviews it to determine:
   - Whether to continue the previous analysis topic
   - What progress has been made
   - What areas need attention
3. Agent proposes either:
   - Continuing the previous topic
   - Starting a new analysis topic based on context

### During Session
- Conversation history is maintained in memory
- Existing compaction strategy handles summarization when context grows large
- Raw conversation text is preserved before compaction occurs
- Agent can query vector DB for relevant past insights when discussing specific topics

### Session End
Sessions end when:
- User explicitly ends the session (via command)
- User switches to a different mode
- Application closes

On session end:
1. Complete session transcript saved to markdown file
2. Final summary generated
3. Session content tokenized and embedded into vector DB

## Storage Structure

### Session Files
```
.project-orb/
  sessions/
    analysis/
      YYYY-MM-DD-HHMMSS-session.md       # Full transcript
      YYYY-MM-DD-HHMMSS-summary.md       # Session summary
```

### Session Markdown Format
```markdown
# Analysis Session - [Date/Time]

## Metadata
- Start: [timestamp]
- End: [timestamp]
- Duration: [duration]
- Topics: [extracted topics]

## Conversation
[Full conversation transcript with timestamps]

## Key Insights
[Extracted during session or at end]

## Action Items
[If any were identified]
```

### Summary Format
```markdown
# Session Summary - [Date/Time]

## Overview
[Brief description of session focus]

## Topics Covered
- Topic 1
- Topic 2

## Key Observations
- Observation 1
- Observation 2

## Recommendations Made
- Recommendation 1
- Recommendation 2

## Progress Notes
[Any progress on previous topics]

## Next Steps
[Suggested areas for next session]
```

## Vector Database

### Purpose
Enable semantic search across all historical analysis sessions to:
- Find relevant past observations when discussing specific topics
- Track progress on recurring themes
- Provide context-aware feedback based on historical patterns

### Chunking Strategy
Content is chunked by semantic units:
- Individual insights/observations
- Recommendations
- User feedback
- Metric discussions
- Topic-based conversation segments

### Metadata Stored with Embeddings
```json
{
  "session_id": "YYYY-MM-DD-HHMMSS",
  "timestamp": "ISO-8601",
  "content_type": "observation|recommendation|metric|feedback",
  "topics": ["topic1", "topic2"],
  "chunk_text": "actual content"
}
```

### Search Strategy
During a session, when discussing specific topics:
1. Extract key terms/concepts from current conversation
2. Query vector DB for semantically similar past content
3. Filter by relevance score (threshold TBD)
4. Present relevant historical context to agent
5. Agent uses this to provide feedback on progress/patterns

## Context Loading

### On Session Start
1. Load most recent session summary (if exists)
2. Query vector DB for any high-priority unresolved items
3. Present context to agent with options:
   - Continue previous analysis
   - Start new topic
   - Review specific past insights

### During Session
- Agent can trigger vector search when:
  - User mentions specific topics/areas
  - Discussing progress or changes
  - Comparing current state to past observations
- Search results provided as additional context
- Agent synthesizes historical and current information

## User Control

### Commands
- `/end-session` - Explicitly end current session and save
- `/session-history` - View past session summaries

## llama.cpp Management

### Configuration
Config file at `~/.config/project-orb/config.yaml`:
```yaml
llama_cpp:
  models_dir: "/path/to/models"
  chat_model: "chat-model.gguf"
  embedding_model: "nomic-embed-text.gguf"
  chat_port: 8080
  embedding_port: 8081
```