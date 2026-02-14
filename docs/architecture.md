# Architecture

## System Overview

DevMemory is a single Go binary that serves as a persistent memory layer for AI development assistants. It connects to Claude Code via the Model Context Protocol (MCP) and stores project knowledge in PostgreSQL with pgvector for semantic search.

```
┌─────────────────────────────────────────────────────────────┐
│                        Claude Code                          │
│                    (AI Development Assistant)                │
└────────────────────────────┬────────────────────────────────┘
                             │ MCP (stdio / SSE)
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                     DevMemory Server                        │
│                        (Go binary)                          │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────┐  │
│  │ MCP Layer│  │  Store   │  │Embedding │  │    Web    │  │
│  │ 14 tools │  │Interface │  │  Client  │  │ Dashboard │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬─────┘  │
│       │              │             │               │        │
└───────┼──────────────┼─────────────┼───────────────┼────────┘
        │              │             │               │
        ▼              ▼             ▼               ▼
   Usage Stats    PostgreSQL    Embed-Svc      Browser
                  + pgvector    (ONNX)         (HTMX)
                    :5434        :8091          :8090
```

## Transport Modes

The server supports three transport modes, selected by the `TRANSPORT` environment variable:

### stdio (default)

Used by Claude Code. The MCP server reads JSON-RPC messages from stdin and writes responses to stdout. Claude Code spawns the devmemory process as a child process, configured via `.mcp.json` in the project root.

```
Claude Code ──fork──► devmemory process
                      stdin  ← JSON-RPC requests
                      stdout → JSON-RPC responses
                      stderr → log output (slog)
```

### SSE (Server-Sent Events)

For remote access. Starts an HTTP server that implements the MCP SSE transport. Multiple clients can connect simultaneously.

```
Remote Client ──HTTP──► :8090/sse
                        POST /message → tool calls
                        GET  /sse     → event stream
```

### Web (Dashboard)

Starts a full web application with an HTMX-powered dashboard. Does not expose MCP tools — this mode is for monitoring and management only.

```
Browser ──HTTP──► :8090
                  GET /           → Dashboard (live stats)
                  GET /search     → Semantic search
                  GET /history    → Session browser
                  GET /memories   → Memory CRUD
                  GET /api/*      → HTMX fragment endpoints
```

## Data Model

### Entity Relationship

```
projects (1)
    │
    ├──► memories (N)      key-value pairs grouped by topic
    │    └── embedding      384-dim vector
    │
    ├──► sessions (N)      numbered transcripts
    │    └── embedding      384-dim vector
    │
    ├──► file_index (N)    source file signatures
    │    └── embedding      384-dim vector
    │
    └──► usage_stats (N)   query tracking
```

### Tables

**projects** — Multi-project registry. Each project has an ID, name, root path, and optional JSON metadata.

**memories** — The core knowledge store. Memories are organized by `project_id` + `topic` + `key`, with a `value` field and a 384-dimension vector embedding. Topics group related memories (e.g., "architecture", "lessons", "decisions"). Unique constraint on `(project_id, topic, key)` — writes are UPSERT.

**sessions** — Numbered session transcripts. Each session has a title, optional summary, optional full content, and metadata. Unique constraint on `(project_id, session_num)`. Full-text search indexes cover title + summary + content.

**file_index** — Source file signatures. Stores file path, type, a JSON array of symbols (function/type names), and a summary. Unique constraint on `(project_id, file_path)`. Used for semantic code discovery without reading full files.

**usage_stats** — Query analytics. Records every MCP tool call with the tool name, query text, result count, and estimated tokens saved. Powers the dashboard's savings calculator.

### Indexes

Each content table has three types of indexes:

| Index Type | Purpose | Implementation |
|------------|---------|----------------|
| **HNSW Vector** | Semantic similarity search | `vector_cosine_ops` on `embedding` column |
| **GIN Full-Text** | Keyword search fallback | `tsvector` generated column |
| **B-tree Composite** | Exact lookups | `(project_id, topic, key)` etc. |

Total: 16 indexes across 5 tables.

## Embedding Pipeline

```
MCP Tool Call (e.g., memory_set)
    │
    ▼
Extract text content (value, summary, symbols)
    │
    ▼
POST http://embed-svc:8091/embed
    body: {"text": "..."}
    │
    ▼
ONNX Runtime (all-MiniLM-L6-v2)
    │
    ▼
384-dim float32 vector
    │
    ▼
Store in PostgreSQL vector(384) column
```

**Embedding service details:**
- Model: `sentence-transformers/all-MiniLM-L6-v2` (86MB ONNX)
- Dimensions: 384
- Max tokens: 128 (truncated)
- Runtime: ONNX Runtime (Python 3.12 sidecar)
- Fallback: If embedding URL is empty or service is down, queries use keyword-only search

**On write**: Every `memory_set`, `session_create`, and `file_index` call generates an embedding for the content and stores it alongside the text.

**On search**: The query text is embedded, then used for cosine similarity search against the HNSW index. Results from vector search and FTS are merged by score.

## Hybrid Search

Every search query runs two parallel strategies:

```
Search Query: "how does authentication work?"
    │
    ├──► Vector Search (cosine similarity)
    │    SELECT *, 1 - (embedding <=> $query_vec) AS score
    │    FROM memories
    │    WHERE project_id = $pid
    │    ORDER BY embedding <=> $query_vec
    │    LIMIT $limit
    │
    └──► Full-Text Search (tsvector)
         SELECT *, ts_rank(search_vector, query) AS score
         FROM memories
         WHERE project_id = $pid
           AND search_vector @@ plainto_tsquery('english', $query)
         ORDER BY score DESC
         LIMIT $limit

Results merged by score, duplicates removed
```

**Graceful degradation:**
- Embedding service available → hybrid (vector + FTS)
- Embedding service down → FTS only
- No matching FTS results → vector only
- Empty database → empty results

## Web Dashboard

### GOTH Stack

The dashboard uses Go server-side rendering with HTMX for interactivity:

- **Go `html/template`**: Server-rendered HTML with clone-per-page template pattern
- **HTMX**: Client-side interactivity via `hx-get`, `hx-trigger`, `hx-swap` attributes
- **Tailwind CSS**: Styling via CDN (no build step, dark theme)

### Template Architecture

Go's `html/template` has a namespace collision when multiple pages define `{{define "content"}}`. DevMemory solves this with a **clone-per-page** pattern:

```go
// 1. Parse base templates (layout + all fragments)
base := template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS,
    "templates/layout.html",
    "templates/_*.html",
))

// 2. Clone base for each page, parse page template into the clone
for _, page := range pages {
    clone := template.Must(base.Clone())
    template.Must(clone.ParseFS(templateFS, "templates/"+page))
    pageMap[page] = clone
}
```

Each page gets its own isolated template set with the shared layout and fragments.

### Real-Time Updates

The dashboard uses HTMX polling (every 5 seconds) to fetch updated stats, cost panels, and project cards:

```html
<div hx-get="/api/stats" hx-trigger="every 5s" hx-swap="innerHTML">
  <!-- Stats grid auto-refreshes -->
</div>
```

This approach was chosen over SSE because it works reliably across all browsers and transport configurations, especially when the MCP server runs in a separate stdio process.

## MCP Protocol Integration

DevMemory implements the MCP protocol via [mcp-go](https://github.com/mark3labs/mcp-go):

```go
type Server struct {
    store  store.Store
    emb    *embedding.Service
    events EventPublisher    // optional, for dashboard updates
    mcp    *server.MCPServer
}
```

Each of the 14 tools is registered with a name, description, and JSON schema for parameters. The MCP server handles JSON-RPC framing, tool discovery (`tools/list`), and tool execution (`tools/call`).

### Usage Tracking

After every tool handler returns, usage is recorded:

```go
func (s *Server) recordUsage(ctx context.Context, toolName, projectID, query string, resultsCount int) {
    tokens := s.tokenEstimate(toolName, resultsCount)
    _ = s.store.RecordUsage(ctx, &store.UsageStat{
        ProjectID:       projectID,
        ToolName:        toolName,
        QueryText:       query,
        ResultsCount:    resultsCount,
        TokensEstimated: tokens,
    })
    if s.events != nil {
        s.events.Publish("dashboard-stats")
    }
}
```

The `events` field is only set in web transport mode. In stdio mode, usage is still recorded to the database and picked up by the dashboard's polling.

## Configuration

All configuration is via environment variables (12-factor app):

```go
type Config struct {
    DatabaseURL      string // PostgreSQL connection
    Transport        string // stdio, sse, web
    Port             string // Listen port (SSE/web)
    EmbeddingURL     string // External embedding API
    EmbeddingDim     int    // Vector dimensions (384)
    LogLevel         string // debug, info, warn, error
    LogFormat        string // text, json
    MigrationsDir    string // SQL migration files
    MigrateOnStart   bool   // --migrate flag
    ExitAfterMigrate bool   // --exit-after-migrate flag
}
```

## Dependencies

DevMemory intentionally minimizes dependencies:

| Dependency | Purpose |
|------------|---------|
| `github.com/jackc/pgx/v5` | PostgreSQL driver with pgvector support |
| `github.com/mark3labs/mcp-go` | MCP protocol SDK |
| Go stdlib | Everything else (HTTP, templates, JSON, logging, crypto) |

No web framework. No ORM. No build tools. One binary.

## Deployment Topology

### Local Development

```
Docker Compose:
  postgres (pgvector/pgvector:pg16) :5434
  embed-svc (python:3.12-slim)      :8091

Host:
  devmemory (stdio)    — spawned by Claude Code
  devmemory (web)      — dashboard at :8090
```

### Production (Future)

```
AWS / GCP:
  PostgreSQL managed instance (with pgvector)
  Container: devmemory (SSE transport)
  Container: embed-svc
  Load balancer → :8090 (dashboard)
  VPN / IAM → MCP access control
```

See the [team-memory-infra](https://github.com/Platform-LSS/team-memory-infra) repository for AWS Terraform infrastructure that could host DevMemory with RLS, VPN, and encrypted backups.
