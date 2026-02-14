# Features Reference

## MCP Tools

DevMemory exposes 14 tools via the Model Context Protocol. Claude Code discovers these automatically from `.mcp.json`.

---

### Project Management

#### `project_register`

Register a new project for tracking.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | yes | Unique project identifier (e.g., `plss-fhir`) |
| `name` | string | yes | Human-readable project name |
| `root_path` | string | no | Absolute path to project root |

```json
{"name": "project_register", "arguments": {"id": "plss-fhir", "name": "PLSS FHIR Server", "root_path": "/path/to/project"}}
```

#### `project_list`

List all registered projects. No parameters.

Returns: Array of projects with ID, name, root path, and timestamps.

#### `project_status`

Get comprehensive status for a project.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |

Returns: Memory count, session count, file count, recent queries, and token savings.

---

### Memory Management

Memories are key-value pairs organized by **topic**. Each memory has a semantic embedding for search.

#### `memory_set`

Create or update a memory. Uses UPSERT — safe to call repeatedly.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `topic` | string | yes | Topic group (e.g., `architecture`, `lessons`, `decisions`) |
| `key` | string | yes | Unique key within topic |
| `value` | string | yes | Memory content |

```json
{"name": "memory_set", "arguments": {
  "project_id": "plss-fhir",
  "topic": "architecture",
  "key": "database",
  "value": "PostgreSQL 16 + pgvector 0.8.1. JSONB storage for FHIR resources. Clinical tables dual-write via background goroutines."
}}
```

The value is embedded automatically and stored alongside the text.

#### `memory_get`

Retrieve a specific memory by exact topic and key.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `topic` | string | yes | Topic |
| `key` | string | yes | Key |

#### `memory_list`

List all memories for a project, optionally filtered by topic.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `topic` | string | no | Filter by topic (empty = all topics) |

#### `memory_search`

Semantic + keyword search across all memories in a project.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `query` | string | yes | Search query (natural language) |
| `limit` | int | no | Max results (default: 5) |

```json
{"name": "memory_search", "arguments": {
  "project_id": "plss-fhir",
  "query": "how does the search system work",
  "limit": 3
}}
```

Returns: Memories ranked by relevance score (0-1), combining vector similarity and keyword match.

**Token savings**: ~500 tokens per result vs ~5,000+ reading a full doc file.

#### `memory_delete`

Delete a specific memory.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `topic` | string | yes | Topic |
| `key` | string | yes | Key |

---

### Session Management

Sessions represent numbered conversation transcripts with optional full content.

#### `session_create`

Create or update a session transcript.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `session_num` | int | yes | Session number (must be > 0) |
| `title` | string | yes | Session title |
| `summary` | string | no | Brief summary |
| `content` | string | no | Full transcript content |

```json
{"name": "session_create", "arguments": {
  "project_id": "plss-fhir",
  "session_num": 19,
  "title": "P0 Gap Closure — Custom Search Params",
  "summary": "Implemented Brooklyn custom search params, _source parameter, inserted_at sort alias. 17 new tests.",
  "content": "# Session 19: P0 Gap Closure\n\n## What Was Built\n..."
}}
```

The summary (or title if no summary) is embedded for semantic search.

#### `session_get`

Retrieve a specific session by number.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `session_num` | int | yes | Session number |

#### `session_list`

List all sessions for a project, ordered by session number.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |

#### `session_search`

Semantic + keyword search across all sessions in a project.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `query` | string | yes | Search query |
| `limit` | int | no | Max results (default: 5) |

**Token savings**: ~2,000 tokens per result vs ~10,000+ reading a full transcript file.

---

### File Indexing

Index source files with function/type signatures for semantic code discovery.

#### `file_index`

Index a source file. Stores metadata, not the full file content.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `file_path` | string | yes | File path relative to project root |
| `file_type` | string | no | File type (e.g., `go`, `python`, `sql`) |
| `summary` | string | no | One-line description of the file |
| `symbols` | string | no | JSON array of function/type names |

```json
{"name": "file_index", "arguments": {
  "project_id": "plss-fhir",
  "file_path": "internal/handler/patch.go",
  "file_type": "go",
  "summary": "PATCH handler supporting JSON Merge Patch (RFC 7386) and JSON Patch (RFC 6902)",
  "symbols": "[\"HandlePatch\", \"applyMergePatch\", \"applyJSONPatch\"]"
}}
```

#### `file_search`

Semantic + keyword search across indexed files.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_id` | string | yes | Project ID |
| `query` | string | yes | Search query |
| `limit` | int | no | Max results (default: 5) |

```json
{"name": "file_search", "arguments": {
  "project_id": "plss-fhir",
  "query": "PATCH request handling",
  "limit": 3
}}
```

**Token savings**: ~800 tokens per result vs ~2,000+ reading a full source file.

---

## Web Dashboard

### Dashboard Page (`/`)

The main view shows real-time project statistics, updated every 5 seconds via HTMX polling.

**Stats Grid**: Projects, memories, sessions, files — global counts.

**Token Savings Panel**: Shows cumulative savings with period filter (24h, 7d, 30d, all):
- Queries served
- Context tokens saved
- API cost saved (at Sonnet 4.5 pricing: $3/MTok input, $15/MTok output)
- Pro subscription context saved (fraction of 200K window)

**Project Cards**: Per-project breakdown with memory/session/file counts, query count, tokens saved, and API cost saved.

### Search Page (`/search`)

"Ask Anything" interface with debounced semantic search (300ms delay after typing stops).

Searches across all entity types simultaneously:
- Memories (grouped by topic)
- Sessions (with title and score)
- Files (with path and summary)

Each result is expandable via `<details>` to show full content.

### History Page (`/history`)

Session browser with project selector dropdown:
1. Select project → loads session list via HTMX
2. Click session → expands detail panel with full content

### Memories Page (`/memories`)

Full CRUD interface for memories:
- Left sidebar: projects and topics
- Main area: memory cards for selected topic
- Create: form at top with project, topic, key, value fields
- Edit: click pencil icon → inline form swap
- Delete: click trash icon with confirmation dialog

---

## CLI Tools

### `devmemory` — Main Server

```bash
devmemory [flags]

Flags:
  --migrate              Run database migrations on startup
  --exit-after-migrate   Exit after migrations (for CI/CD)
  --migrations-dir DIR   Absolute path to migrations directory
```

Transport is selected by `TRANSPORT` env var (`stdio`, `sse`, `web`).

### `backfill` — Knowledge Loader

Bulk-loads project knowledge into DevMemory with embeddings.

```bash
backfill \
  --project-id=plss-fhir \
  --project-name="PLSS FHIR Server" \
  --root=/path/to/project \
  --db="postgres://..." \
  --embed-url="http://localhost:8091/embed"
```

**What it loads:**

| Source | Stored As | Topic |
|--------|-----------|-------|
| `spec/*.md` | Memories | `spec` |
| `docs/*.md` | Memories | `docs` |
| `docs/adr/*.md` | Memories | `adr` |
| `CLAUDE.md` | Memory | `project/claude-md` |
| `README.md` | Memory | `project/readme` |
| `transcripts/INDEX.md` | Memory | `project/transcript-index` |
| `transcripts/*.md` | Sessions | (numbered 100+) |
| `**/*.go` | File Index | (with function/type extraction) |

**Performance**: 128 items loaded in ~4 seconds (PLSS FHIR project).

### `save-session` — Session Saver

Save a single session transcript.

```bash
save-session \
  --project=plss-fhir \
  --num=19 \
  --title="P0 Gap Closure" \
  --summary="Custom search params, 17 new tests" \
  --file=transcripts/019-p0-gap-closure.md
```

---

## Usage Analytics

### Token Estimation

Every MCP tool call records an estimated token savings based on what the alternative would have been (reading full files):

| Tool | Tokens per Result | Rationale |
|------|-------------------|-----------|
| `memory_search` | 500 | vs ~5K reading a doc/spec file |
| `session_search` | 2,000 | vs ~10K reading a full transcript |
| `file_search` | 800 | vs ~2K reading a source file |
| `memory_get` | 500 | vs finding and reading the right doc |
| `session_get` | 2,000 | vs reading full transcript file |
| `memory_set` | 100 | Write operation, minimal savings |
| `memory_list` | 100 | Listing, minimal savings |
| Other tools | 100 | Utility operations |

### Cost Calculation

The dashboard shows savings in two formats:

**API pricing** (pay-per-token):
- Input: tokens saved x $3/MTok (Sonnet 4.5)
- Output: estimated at tokens saved x $15/MTok

**Pro subscription** (fixed monthly):
- Context efficiency: tokens saved / 20,000 average context per interaction
- Shows how many interactions worth of context window was preserved

---

## Search Modes

### Semantic Search (with embeddings)

When the embedding service is available, search uses cosine distance on HNSW-indexed vectors:

```sql
SELECT *, 1 - (embedding <=> $1) AS score
FROM memories
WHERE project_id = $2
ORDER BY embedding <=> $1
LIMIT $3
```

Returns results ranked by semantic similarity (0-1 scale). Finds conceptually related content even without exact keyword matches.

### Keyword Search (FTS fallback)

When embeddings are unavailable, or as a complement to vector search:

```sql
SELECT *, ts_rank(search_vector, plainto_tsquery('english', $1)) AS score
FROM memories
WHERE project_id = $2
  AND search_vector @@ plainto_tsquery('english', $1)
ORDER BY score DESC
LIMIT $3
```

Uses PostgreSQL's built-in full-text search with English stemming.

### Hybrid (default)

Both searches run in parallel. Results are merged by score, with duplicates removed. This catches both semantically similar content (vector) and exact keyword matches (FTS).

### Cross-Entity Search

The `SearchAll` store method searches across memories, sessions, and files simultaneously for the web dashboard's "Ask Anything" feature. Results are grouped by entity type and sorted by relevance within each group.
