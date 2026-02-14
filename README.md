# DEV-MEMORY-MCP

**Persistent AI development memory with semantic search — an MCP server that saves context tokens and credits by giving Claude targeted recall instead of full-file reads.**

Built with Go, PostgreSQL + pgvector, and an HTMX live dashboard.

---

## Why This Exists

Every Claude Code session starts cold. The AI re-reads files, re-discovers architecture decisions, and re-learns lessons from scratch. On a 56-file Go project with 37 test files, a single "where did we leave off?" question can consume **20,000+ context tokens** loading CLAUDE.md, transcripts, and source files.

**DevMemory fixes this.** It stores project knowledge — architecture decisions, session transcripts, source file signatures, and lessons learned — in a PostgreSQL vector database with semantic embeddings. When Claude needs context, it queries DevMemory and gets a **targeted 500-2,000 token result** instead of loading entire files.

### Real-World Savings

On the [PLSS FHIR Server](https://github.com/Platform-LSS/PLSS-FHIR-SERVER-POC) project (290 tests, 18 sessions, 56 Go files):

| Metric | Without DevMemory | With DevMemory | Savings |
|--------|-------------------|----------------|---------|
| "Where did we leave off?" | ~23,000 tokens | ~2,000 tokens | **91%** |
| "How does search work?" | ~8,000 tokens | ~500 tokens | **94%** |
| "What lessons about goroutines?" | ~5,000 tokens | ~500 tokens | **90%** |
| Architecture recall | Read 3-5 files | 1 memory_search | **80-95%** |

**First real interaction: 27,900 tokens saved** — a single project status query that would have required reading MEMORY.md + CLAUDE.md + transcripts.

### Where It Helps Most

- **Context recall**: "What decisions did we make about X?" — searches memories instead of reading docs
- **Session history**: "What happened in session 12?" — retrieves transcript summary instead of full file
- **Code discovery**: "Which files handle authentication?" — semantic file search instead of grep + read
- **Lesson lookup**: "What went wrong with goroutines?" — pinpoints the exact lesson
- **Project onboarding**: New sessions get full project context from a few MCP queries

### Where It Helps Less

- **Active coding**: Editing files still requires reading exact source code
- **Test execution**: Running tests is Bash, not memory
- **Single-file fixes**: Trivial changes don't benefit from memory lookup

---

## Architecture

```
Claude Code ──stdio──► DevMemory MCP Server (Go)
                              │
                    ┌─────────┼──────────┐
                    ▼         ▼          ▼
              PostgreSQL   Embed-Svc   Web Dashboard
              + pgvector   (ONNX)      (HTMX)
               :5434        :8091       :8090
```

**Three transport modes** — single binary, selected by `TRANSPORT` env var:

| Mode | Use Case | Protocol |
|------|----------|----------|
| `stdio` (default) | Claude Code integration | MCP over stdin/stdout |
| `sse` | Remote/shared access | MCP over HTTP SSE |
| `web` | Live dashboard | HTTP + HTMX polling |

**Hybrid search** — every query runs both:
1. **Semantic search**: pgvector HNSW cosine similarity (384-dim embeddings from all-MiniLM-L6-v2)
2. **Full-text search**: PostgreSQL tsvector with GIN indexes

Results are merged by score. If the embedding service is unavailable, gracefully falls back to keyword-only search.

See [docs/architecture.md](docs/architecture.md) for the full design.

---

## Features

### 14 MCP Tools — Available Now

DevMemory provides 14 tools via the Model Context Protocol that Claude Code discovers automatically. Each tool is designed to return targeted results that reduce context token consumption.

#### Project Management (3 tools)

| Tool | What It Does | Token Impact |
|------|-------------|--------------|
| `project_register` | Register a project with ID, name, and root path | Enables scoped queries across multiple projects |
| `project_list` | List all registered projects with metadata | Single call vs reading multiple config files |
| `project_status` | Get memory/session/file counts, recent queries, savings | Full project overview in ~200 tokens |

#### Memory Management (5 tools)

Memories are key-value pairs organized by **topic** (e.g., `architecture`, `lessons`, `decisions`). Every memory is embedded for semantic search on write.

| Tool | What It Does | Token Impact |
|------|-------------|--------------|
| `memory_set` | Store a memory with project, topic, key, value. UPSERT — safe to repeat. | Write operation, auto-embeds for future search |
| `memory_get` | Retrieve a specific memory by exact topic + key | **~500 tokens** vs ~5,000 reading a full doc |
| `memory_list` | List all memories for a project, optionally filtered by topic | Browse available knowledge without loading files |
| `memory_search` | **Semantic + keyword search** across all memories in a project | **~500 tokens/result** vs reading 3-5 source files |
| `memory_delete` | Remove a specific memory | Housekeeping |

**Example — saves ~4,500 tokens:**
```
Without DevMemory:  Claude reads CLAUDE.md (3K) + architecture doc (5K) + ADR (2K) = ~10K tokens
With DevMemory:     memory_search("database architecture") → 3 results = ~1,500 tokens
```

#### Session Management (4 tools)

Sessions are numbered conversation transcripts — the full history of what was built, decided, and learned across development sessions.

| Tool | What It Does | Token Impact |
|------|-------------|--------------|
| `session_create` | Save a session with number, title, summary, and optional full content | Captures session context for future recall |
| `session_get` | Retrieve a specific session by number | **~2,000 tokens** vs ~10,000 reading a full transcript |
| `session_list` | List all sessions with titles and dates | Quick overview of project history |
| `session_search` | **Semantic + keyword search** across all session transcripts | **~2,000 tokens/result** vs loading multiple transcripts |

**Example — saves ~21,000 tokens:**
```
Without DevMemory:  "Where did we leave off?" → Claude reads MEMORY.md (2K) + CLAUDE.md (3K) +
                    last 2 transcripts (9K each) = ~23,000 tokens
With DevMemory:     session_search("latest progress") → 2 results = ~4,000 tokens
                    Actual first interaction: 27,900 tokens saved
```

#### File Indexing (2 tools)

Index source files with function/type signatures for semantic code discovery — find relevant files without reading them all.

| Tool | What It Does | Token Impact |
|------|-------------|--------------|
| `file_index` | Index a file with path, type, symbols (functions/types), and summary | Build searchable index of entire codebase |
| `file_search` | **Semantic search** across indexed files by meaning | **~800 tokens/result** vs ~2,000+ reading each file |

**Example — saves ~6,000 tokens:**
```
Without DevMemory:  "Which files handle PATCH?" → Claude runs Grep, reads 4 files = ~8,000 tokens
With DevMemory:     file_search("PATCH request handling") → 3 results = ~2,400 tokens
```

### Hybrid Search Engine

Every search query runs **two strategies in parallel**:

1. **Semantic search** — pgvector HNSW cosine similarity on 384-dim embeddings. Finds conceptually related content even without exact keyword matches.
2. **Full-text search** — PostgreSQL tsvector with English stemming. Catches exact terminology and acronyms.

Results are merged by score. If the embedding service is unavailable, gracefully falls back to keyword-only search. No query ever fails.

### Web Dashboard (GOTH Stack)

Live dashboard at `:8090` built with Go `html/template` + HTMX + Tailwind CSS (CDN, no build step). All panels auto-update every 5 seconds.

| Page | What It Shows |
|------|--------------|
| **Dashboard** | Real-time stats grid (projects, memories, sessions, files), per-project cards with query counts + tokens saved + API cost saved, token savings calculator with both API pricing ($3/MTok) and Pro subscription context metrics |
| **Search** | "Ask Anything" — debounced semantic search across memories, sessions, and files simultaneously, with relevance scores and expandable result details |
| **History** | Session browser — select project, browse session list, drill down to full transcript content |
| **Memories** | Full CRUD — browse by project/topic, create new memories, inline edit, delete with confirmation |

### CLI Tools

| Command | What It Does |
|---------|-------------|
| `devmemory` | Main MCP server — runs in stdio (Claude Code), SSE (remote), or web (dashboard) mode |
| `backfill` | Bulk-load project knowledge: specs, docs, ADRs as memories; transcripts as sessions; Go files as file index. All with semantic embeddings. **128 items in 4 seconds.** |
| `save-session` | Save a single session transcript with title, summary, and optional file content |

### Usage Analytics & Savings Tracking

Every MCP tool call is tracked automatically with token estimation heuristics:

| Tool Type | Est. Tokens Saved per Result | vs Alternative |
|-----------|------------------------------|----------------|
| `memory_search` | 500 | Reading a full doc/spec file (~5K) |
| `session_search` | 2,000 | Reading a full transcript (~10K) |
| `file_search` | 800 | Reading a source file (~2K) |
| `memory_get` | 500 | Finding and reading the right doc |
| `session_get` | 2,000 | Reading a full transcript file |
| Other tools | 100 | Utility operations |

The dashboard shows cumulative savings in two formats:

- **API cost** — tokens saved x $3/MTok input, x $15/MTok output (Sonnet 4.5 pricing)
- **Pro subscription** — tokens saved as a fraction of the 200K context window per interaction

### Multi-Project Support

All data is scoped by `project_id`. Register multiple projects and search each independently. The backfill tool loads entire project codebases in seconds. The dashboard shows per-project stats cards with individual savings tracking.

### Three Transport Modes

Single binary, three ways to run:

| Mode | Use Case | How |
|------|----------|-----|
| `stdio` | Claude Code integration | MCP over stdin/stdout — Claude Code spawns it from `.mcp.json` |
| `sse` | Remote/shared access | MCP over HTTP Server-Sent Events |
| `web` | Live dashboard | HTTP + HTMX with 5-second polling |

Run stdio and web simultaneously — stdio for Claude Code, web for monitoring savings in real-time.

See [docs/features.md](docs/features.md) for full tool reference with parameter tables and examples.

---

## Quick Start

### Prerequisites

- Go 1.24+
- Docker & Docker Compose
- (Optional) Claude Code CLI for MCP integration

### 1. Start Infrastructure

```bash
cd devmemory
docker compose up -d postgres embed-svc
```

This starts:
- **PostgreSQL 16 + pgvector** on port 5434
- **Embedding service** (all-MiniLM-L6-v2, ONNX) on port 8091

### 2. Build & Run

```bash
# Build
go build -o devmemory ./cmd/devmemory/

# Run with migrations (stdio mode for Claude Code)
DATABASE_URL="postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable" \
EMBEDDING_URL="http://localhost:8091/embed" \
./devmemory --migrate

# Or run the web dashboard
TRANSPORT=web PORT=8090 \
DATABASE_URL="postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable" \
EMBEDDING_URL="http://localhost:8091/embed" \
./devmemory --migrate
```

### 3. Connect Claude Code

Create `.mcp.json` in your project root:

```json
{
  "mcpServers": {
    "devmemory": {
      "command": "/path/to/devmemory",
      "args": ["--migrate", "--migrations-dir", "/path/to/devmemory/migrations"],
      "env": {
        "DATABASE_URL": "postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable",
        "EMBEDDING_URL": "http://localhost:8091/embed"
      }
    }
  }
}
```

> **Important**: The `--migrations-dir` must be an absolute path. Claude Code spawns the MCP process from your project directory, not the DevMemory source directory.

Restart Claude Code. DevMemory tools will appear automatically.

### 4. Load Project Knowledge

```bash
go build -o backfill ./cmd/backfill/

./backfill \
  --project-id=my-project \
  --project-name="My Project" \
  --root=/path/to/project \
  --db="postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable" \
  --embed-url="http://localhost:8091/embed"
```

The backfill tool loads:
- `spec/` and `docs/` as memories (by topic)
- `CLAUDE.md`, `README.md` as memories
- `transcripts/` as numbered sessions
- All `.go` files with function/type signatures

### 5. Instruct Claude to Use DevMemory

Add to your project's `CLAUDE.md`:

```markdown
## DevMemory — ALWAYS USE FIRST

Before reading files or searching the codebase, ALWAYS use DevMemory MCP tools first.
This saves thousands of context tokens per query.

- `memory_search`: Architecture decisions, lessons, patterns
- `file_search`: Find Go functions/types by meaning
- `session_search`: Past decisions and implementation context

Only fall back to Glob/Grep/Read for exact line-by-line code editing.
```

---

## Build & Development

```bash
make build           # Build all binaries
make test            # Run tests
make run             # Run in stdio mode
make docker-up       # Start PostgreSQL + embed-svc
make docker-down     # Stop containers
make migrate         # Run migrations only
make clean           # Remove binaries
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable` | PostgreSQL connection string |
| `TRANSPORT` | `stdio` | Transport: `stdio`, `sse`, or `web` |
| `PORT` | `8090` | Listen port (SSE/web modes) |
| `EMBEDDING_URL` | _(empty)_ | Embedding API URL; empty = keyword search only |
| `EMBEDDING_DIM` | `384` | Vector dimension (matches all-MiniLM-L6-v2) |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `LOG_FORMAT` | `text` | Log format: text or json |

---

## Database

**PostgreSQL 16 + pgvector 0.8.1** with 5 tables:

| Table | Purpose | Indexes |
|-------|---------|---------|
| `projects` | Multi-project registry | PK |
| `memories` | Key-value with topic grouping | HNSW vector, GIN FTS, B-tree composite |
| `sessions` | Numbered transcripts | HNSW vector, GIN FTS, B-tree composite |
| `file_index` | Source file signatures | HNSW vector, GIN FTS, B-tree composite |
| `usage_stats` | Query tracking for analytics | B-tree on project, time, tool |

All vector columns are `vector(384)` with HNSW indexes using `vector_cosine_ops`. All text content has generated `tsvector` columns with GIN indexes for full-text search fallback.

---

## Project Structure

```
devmemory/
├── cmd/
│   ├── devmemory/main.go      # Main MCP server entry point
│   ├── backfill/main.go       # Bulk knowledge loader
│   └── save-session/main.go   # Single session saver
├── internal/
│   ├── config/config.go       # Environment configuration
│   ├── embedding/service.go   # External embedding API client
│   ├── mcp/server.go          # 14 MCP tool handlers + usage tracking
│   ├── store/
│   │   ├── store.go           # Store interface (23 methods)
│   │   ├── postgres.go        # PostgreSQL implementation
│   │   └── migrations.go      # Migration runner
│   └── web/
│       ├── server.go          # Web routes + page handlers
│       ├── handlers_api.go    # HTMX fragment handlers
│       ├── templates.go       # Clone-per-page template loader
│       ├── events.go          # In-memory EventBus pub/sub
│       ├── middleware.go      # Request logger
│       └── templates/         # 13 HTML templates (4 pages + 9 fragments)
├── migrations/
│   ├── 001_initial_schema.sql # Core tables + pgvector indexes
│   ├── 002_fix_session_fts.sql
│   └── 003_usage_stats.sql    # Analytics table
├── docker-compose.yml         # PostgreSQL + embed-svc + devmemory
├── Dockerfile                 # Multi-stage Go build
├── Makefile
├── go.mod                     # 2 direct deps: pgx, mcp-go
├── CLAUDE.md                  # AI assistant instructions
└── docs/
    ├── architecture.md        # System design deep-dive
    └── features.md            # Tool reference + usage examples
```

**~2,300 lines of Go** | **95 lines of SQL** | **596 lines of HTML templates** | **2 direct dependencies**

---

## Future Enhancements

### Self-Hosted LLM Pipeline (Qwen-Coder Integration)

The highest-impact enhancement is adding a **local LLM pipeline** between Claude and DevMemory, using self-hosted models like [Qwen2.5-Coder](https://huggingface.co/Qwen/Qwen2.5-Coder-32B) to handle routine queries without consuming Claude API tokens or Pro subscription credits.

```
Claude Code ──MCP──► DevMemory
                        │
                        ├──► PostgreSQL (memory/session/file search)
                        │
                        └──► Qwen-Coder (local, GPU)
                             ├── Summarize search results before returning
                             ├── Answer simple "what/where/how" questions directly
                             ├── Generate embeddings locally (replace ONNX sidecar)
                             └── Pre-filter results by relevance
```

**How it saves money:**

| Query Type | Current (Claude handles) | With Qwen Pipeline |
|------------|--------------------------|---------------------|
| "What architecture decisions about DB?" | Claude reads memory_search results (~500 tokens input) | Qwen summarizes to ~100 tokens, Claude gets a concise answer |
| "Which files handle PATCH?" | Claude reads file_search results (~800 tokens) | Qwen returns file list with one-line descriptions (~200 tokens) |
| "Summarize session 12" | Claude reads full session (~2,000 tokens) | Qwen summarizes to ~300 tokens |
| Embedding generation | External ONNX sidecar | Qwen generates embeddings natively |

**Estimated additional savings: 50-70%** on context tokens for recall-type queries, on top of the 80-95% savings DevMemory already provides.

**Implementation plan:**
1. Add `LOCAL_LLM_URL` config pointing to Qwen-Coder (vLLM/Ollama/llama.cpp)
2. New MCP tool: `smart_search` — routes to Qwen for summarization before returning
3. Qwen generates embeddings via its encoder, replacing the ONNX sidecar
4. Confidence-based routing: simple queries go to Qwen, complex ones pass through to Claude

### Additional Planned Enhancements

| Enhancement | Description | Impact |
|-------------|-------------|--------|
| **Multi-user RLS** | Row-level security per user (PostgreSQL RLS policies) | Team collaboration |
| **Auto-transcript save** | Hook into Claude Code session events to auto-save transcripts | Zero-manual-effort memory |
| **Incremental file sync** | Watch filesystem for changes, auto-re-index modified files | Always-current file index |
| **Cross-project search** | Search across all projects simultaneously | Organization-wide recall |
| **Memory decay** | Score memories by recency + frequency, auto-archive stale ones | Keep context relevant |
| **Export/import** | Dump/restore project knowledge as portable JSON | Backup + sharing |
| **Webhook notifications** | Push events on memory changes (Slack, email) | Team awareness |
| **Query caching** | Cache frequent queries with TTL | Reduce DB load |
| **Embedding model swap** | Support multiple embedding models (BGE, Nomic, etc.) | Flexibility |
| **PostgreSQL LISTEN/NOTIFY** | Replace HTMX polling with real PG-level change notifications | True real-time dashboard |

---

## Tech Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Server | Go 1.24+ `net/http` | Single binary, zero framework deps, stdlib routing |
| Database | PostgreSQL 16 + pgvector 0.8.1 | Battle-tested, HNSW indexes, managed by every cloud provider |
| MCP Protocol | [mcp-go](https://github.com/mark3labs/mcp-go) | Go SDK for Model Context Protocol |
| Embeddings | all-MiniLM-L6-v2 (ONNX Runtime) | 86MB model, 384-dim, fast inference, good quality |
| Web | Go `html/template` + HTMX + Tailwind CDN | No build step, no Node.js, server-rendered |
| Driver | pgx v5 | Native Go PostgreSQL driver with pgvector support |

**Total direct dependencies: 2** (`pgx` and `mcp-go`). Everything else is Go stdlib.

---

## License

[MIT](LICENSE)

---

Built with Claude Code. Token savings tracked in real-time at `http://localhost:8090`.
