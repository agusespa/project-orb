# Analysis Mode Memory System

## Overview

Analysis mode has a tiered memory system for carrying context across sessions. It uses:
- markdown transcript files
- markdown summary files
- embedding vectors saved as JSON sidecar files
- model-invoked memory tools during the analysis step

## How Memory Works

The system has three main parts:

1. Session summaries
When an analysis session is explicitly wrapped, the app generates a structured summary and saves it.

2. Embeddings
That saved summary is embedded with the local embedding model and the vector is stored on disk.

3. Retrieval
Later, when the user returns to Analysis mode or sends a new message, the model can choose to search saved analysis session summaries and load a supporting transcript excerpt through tools exposed by the app.

## Request Flow

Analysis mode uses a two-step response pipeline:

1. `GenerateAnalysis`
The model gets the current session summary and recent turns.
If it needs cross-session recall, it can call tools before producing a short internal analysis.

2. `GenerateResponse`
The app sends that internal analysis back to the model and streams the final user-facing response.

This keeps tool use in the planning stage and keeps the streamed response path simple.

## Memory Tools

Analysis mode currently exposes two tools:

1. `search_memories`
Searches prior saved session summaries semantically and returns matching session ids, summaries, and similarity scores.

2. `load_memory_excerpt`
Loads a short transcript excerpt for a previously found session when the model wants exact prior details.

These tools are only available in Analysis mode. Tool access is mode-scoped so each mode sees only the capabilities that fit its job.

## Startup Behavior

When Analysis mode starts, the app uses a two-step startup flow.

1. It shows a hard-coded welcome message first. This is not generated dynamically.
2. After the welcome:
  - if there is **no saved summary**, the app shows another hard-coded message explaining that there is no previous saved session and suggesting a few natural ways to begin
  - if there **is** a saved summary, the app sends that summary to the model and asks it to suggest a few strong places to start the new session

## Save Behavior

analysis sessions are **not** saved automatically.
If the current analysis session has unsaved turns, the UI warns that the session will be discarded unless the user wraps it first.

### What Saves a Session

Only the `/wrap` command saves the current analysis session.

When `/wrap` runs:

1. The current session is finalized
2. A structured summary is generated
3. The full transcript is written to disk
4. The summary is written to disk
5. An embedding for the summary is generated
6. The embedding vector is written to disk as JSON
7. The app quits

## Storage Structure

The app stores analysis-mode memory in the app data directory, under the analysis sessions subtree.

Typical files look like this:

```text
<app-data-dir>/
  sessions/
    analysis/
      YYYY-MM-DD-HHMMSS-session.md
      YYYY-MM-DD-HHMMSS-summary.md
      YYYY-MM-DD-HHMMSS-summary.embedding.json
```

The exact app data base directory follows the app's configured local data path resolution.

## What Gets Stored

### Transcript File

The transcript file stores:

- session timestamp
- mode
- saved summary
- completed conversation turns

### Summary File

The summary file stores the structured session summary.

The current summary prompt asks for these sections:

- `## Overview`
- `## Emotional Context`
- `## Patterns`
- `## Decisions`
- `## Open Questions`

### Embedding File

The embedding file stores the summary embedding vector as JSON.

This is the vector used later for semantic retrieval.

## How Semantic Retrieval Works

When the app needs relevant past context:

1. It embeds the current query text
2. It loads saved summary embeddings from disk
3. It computes similarity between the query vector and each saved summary vector
4. It ranks the summaries by cosine similarity
5. The model decides whether it should call `search_memories`
6. If the model needs exact prior detail, it can call `load_memory_excerpt` for a returned session id
7. Tool results are fed back into the model during the Analysis step before the final response is written

## Why This Shape

This design is intentionally simple:

- the app stores and retrieves memory data
- the model decides when memory lookup is needed
- tools run before the final response is streamed
- only Analysis mode gets memory tools right now

That avoids hard-coded retrieval heuristics while keeping the response pipeline understandable and debuggable.

## Adding More Tools

When adding new tools:

1. Implement the tool in the agent layer
2. Add it to the relevant mode's `ToolNames`
3. Expose it during the analysis step for that mode
4. Keep the final response step focused on writing the answer, not planning
