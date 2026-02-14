-- DevMemory: AI Development Memory System
-- PostgreSQL 16 + pgvector

CREATE EXTENSION IF NOT EXISTS vector;

-- Projects table: multi-project support
CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    root_path   TEXT,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);

-- Memories: key-value entries with semantic embeddings
CREATE TABLE memories (
    id          BIGSERIAL PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    topic       TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    embedding   vector(384),
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now(),
    created_by  TEXT DEFAULT '',
    UNIQUE(project_id, topic, key)
);

-- Sessions: transcripts with semantic embeddings
CREATE TABLE sessions (
    id          BIGSERIAL PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_num INTEGER NOT NULL,
    title       TEXT NOT NULL,
    summary     TEXT DEFAULT '',
    content     TEXT DEFAULT '',
    embedding   vector(384),
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT now(),
    UNIQUE(project_id, session_num)
);

-- File index: project files with metadata and embeddings
CREATE TABLE file_index (
    id           BIGSERIAL PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_path    TEXT NOT NULL,
    file_type    TEXT DEFAULT '',
    symbols      JSONB DEFAULT '[]',
    summary      TEXT DEFAULT '',
    embedding    vector(384),
    last_indexed TIMESTAMPTZ DEFAULT now(),
    UNIQUE(project_id, file_path)
);

-- HNSW indexes for semantic search
CREATE INDEX idx_memories_embedding ON memories
    USING hnsw (embedding vector_cosine_ops);
CREATE INDEX idx_sessions_embedding ON sessions
    USING hnsw (embedding vector_cosine_ops);
CREATE INDEX idx_files_embedding ON file_index
    USING hnsw (embedding vector_cosine_ops);

-- B-tree indexes for keyword lookups
CREATE INDEX idx_memories_project_topic ON memories(project_id, topic);
CREATE INDEX idx_memories_project_key ON memories(project_id, key);
CREATE INDEX idx_sessions_project ON sessions(project_id, session_num);
CREATE INDEX idx_files_project ON file_index(project_id, file_path);

-- Full-text search indexes
CREATE INDEX idx_memories_fts ON memories
    USING GIN (to_tsvector('english', value));
CREATE INDEX idx_sessions_fts ON sessions
    USING GIN (to_tsvector('english', coalesce(summary, '') || ' ' || coalesce(content, '')));
CREATE INDEX idx_files_fts ON file_index
    USING GIN (to_tsvector('english', coalesce(summary, '')));
