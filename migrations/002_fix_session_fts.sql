-- Update session FTS index to include title
DROP INDEX IF EXISTS idx_sessions_fts;
CREATE INDEX idx_sessions_fts ON sessions
    USING GIN (to_tsvector('english', coalesce(title, '') || ' ' || coalesce(summary, '') || ' ' || coalesce(content, '')));
