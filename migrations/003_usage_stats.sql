-- Usage statistics for web dashboard
CREATE TABLE IF NOT EXISTS usage_stats (
    id               BIGSERIAL PRIMARY KEY,
    project_id       TEXT REFERENCES projects(id) ON DELETE CASCADE,
    tool_name        TEXT NOT NULL,
    query_text       TEXT DEFAULT '',
    results_count    INTEGER DEFAULT 0,
    tokens_estimated INTEGER DEFAULT 0,
    created_at       TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_stats_project ON usage_stats(project_id);
CREATE INDEX IF NOT EXISTS idx_usage_stats_created ON usage_stats(created_at);
CREATE INDEX IF NOT EXISTS idx_usage_stats_tool ON usage_stats(tool_name);
