# Performance Review Memory
Performance review mode uses a lightweight recent-state memory instead of the full analysis-mode memory system.

## Shape
- one rolling Markdown file at `sessions/performance-review/current.md` (which includes session metadata and the summary text)
- a small number of recent timestamped summary snapshots (e.g., `YYYY-MM-DD-HHMMSS-summary.md`)
*(Note: Full conversation transcripts and semantic embeddings are not saved for this mode.)*

## Save Behavior
When a performance review session is persisted, the app:
1. Generates an updated rolling summary via the Mode Finalizer (running an LLM prompt against past summary + new turns)
2. Renders the metadata and summary text as a Markdown document
3. Writes this Markdown document to `current.md` (overwriting the previous)
4. Writes an exact copy as a timestamped snapshot file (`[SessionID]-summary.md`)
5. Prunes older snapshots, retaining only the 4 most recent files

This keeps the memory easy to load, easy to inspect, and easy to replace later when external data sources are added.