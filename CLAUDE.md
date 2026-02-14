# DevMemory — AI Development Memory Server

MCP server for persistent project memory with semantic search. Built in Go, backed by PostgreSQL + pgvector.

## Quick Reference

| Item | Value |
|------|-------|
| Language | Go 1.24+ (stdlib `net/http`, `pgx`, `slog`) |
| Database | PostgreSQL 16 + pgvector 0.8.1 |
| Protocol | MCP (Model Context Protocol) via `mcp-go` |
| Transport | stdio (local) or SSE (remote/cloud) |
| Embeddings | all-MiniLM-L6-v2 via ONNX Runtime (384-dim), keyword FTS fallback |

## What It Does

Provides 14 MCP tools for managing project memory, session transcripts, and file indexes with optional semantic vector search:

### Project Tools
- `project_register` — Register a project for tracking
- `project_list` — List all registered projects
- `project_status` — Get memory/session counts, embedding status

### Memory Tools
- `memory_set` — Store key-value memory with auto-embedding
- `memory_get` — Retrieve by topic/key
- `memory_list` — List by project/topic
- `memory_search` — Semantic or full-text search
- `memory_delete` — Remove a memory entry

### Session Tools
- `session_create` — Create/update transcript with auto-embedding
- `session_get` — Retrieve by session number
- `session_list` — List all sessions
- `session_search` — Semantic or full-text search

### File Index Tools
- `file_index` — Index file with metadata and summary
- `file_search` — Semantic or full-text search over files

## Commands

```bash
make build              # Build Go binary
make test               # Run tests
make run                # Build + run with --migrate
make docker-up          # Start Docker stack (PostgreSQL + server)
make docker-down        # Stop Docker stack
make migrate            # Run migrations only
make clean              # Remove binary + Docker volumes
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable` | PostgreSQL connection |
| `TRANSPORT` | `stdio` | Transport: `stdio` (local), `sse` (remote), or `web` (dashboard) |
| `PORT` | `8090` | Listen port for SSE or web transport |
| `EMBEDDING_URL` | (empty) | External embedding API URL. Empty = keyword search only |
| `EMBEDDING_DIM` | `384` | Embedding vector dimension |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `LOG_FORMAT` | `text` | Log format: text or json |

## Claude Code Integration

### Local (stdio transport)

```json
{
  "mcpServers": {
    "devmemory": {
      "command": "/path/to/devmemory",
      "args": ["--migrate"],
      "env": {
        "DATABASE_URL": "postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable",
        "EMBEDDING_URL": "http://localhost:8091/embed"
      }
    }
  }
}
```

### Remote (SSE transport)

```json
{
  "mcpServers": {
    "devmemory": {
      "url": "http://your-server:8090/sse"
    }
  }
}
```

### Web Dashboard (`TRANSPORT=web`)

```bash
TRANSPORT=web PORT=8090 DATABASE_URL="postgres://..." EMBEDDING_URL="http://localhost:8091/embed" ./devmemory --migrate
open http://localhost:8090
```

GOTH stack (Go html/template + HTMX + Tailwind CSS) dashboard with 4 pages:
- **Dashboard** (`/`) — Real-time stats via SSE, project cards, token savings calculator
- **Search** (`/search`) — "Ask Anything" with debounced semantic search across all entities
- **History** (`/history`) — Session browser with drill-down to full transcripts
- **Memories** (`/memories`) — Browse, create, edit, delete memories by project/topic

Real-time updates use HTMX SSE extension — no polling. Dashboard stats refresh automatically when any MCP tool fires. All styling via Tailwind CDN dark theme, no build step required.

## Architecture

```
Claude Code ──stdio/SSE──► DevMemory (Go binary) ──HTTP──► embed-svc (ONNX)
     Browser ──HTTP/SSE──►       │                           all-MiniLM-L6-v2
                                 ▼                           384-dim vectors
                    PostgreSQL 16 + pgvector
                    ├── projects
                    ├── memories      (+ HNSW vector index)
                    ├── sessions      (+ HNSW vector index)
                    ├── file_index    (+ HNSW vector index)
                    └── usage_stats   (query tracking)
```

### Docker Compose Stack

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| `postgres` | `pgvector/pgvector:pg16` | 5434 | Data + vector storage |
| `embed-svc` | Python 3.12-slim + ONNX | 8091 | Embedding generation |
| `devmemory` | Go 1.24 alpine | 8090 | MCP server (SSE mode) |

### Embedding Service (`embed-svc/`)

Lightweight Python sidecar (~14MB ONNX Runtime vs ~500MB PyTorch):
- Model: `sentence-transformers/all-MiniLM-L6-v2` (86MB ONNX, 384-dim output)
- Tokenizer: HuggingFace `tokenizers` library (Rust-backed, fast)
- Inference: ONNX Runtime with CPUExecutionProvider
- Post-processing: Mean pooling + L2 normalization
- Max tokens: 128 (configurable via `MAX_LENGTH`)
- API: `POST /embed {"text":"..."}` → `{"embedding":[...],"dim":384}`
- Health: `GET /health` → `{"status":"ok","model":"all-MiniLM-L6-v2","dim":384}`

## Key Conventions

- All data stored per-project (multi-project support)
- Embeddings require `embed-svc` running on port 8091 (`docker compose up embed-svc`)
- Without `EMBEDDING_URL`, falls back to PostgreSQL full-text search (`websearch_to_tsquery`)
- Semantic search uses cosine distance via pgvector HNSW indexes
- Memories use topic/key namespacing (e.g. topic=architecture, key=database)
- Sessions are numbered per-project
- UPSERT semantics on all writes (idempotent)
- Migrations run automatically with `--migrate` flag or `make migrate`
