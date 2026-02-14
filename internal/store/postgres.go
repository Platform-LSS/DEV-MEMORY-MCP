package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	slog.Info("connected to PostgreSQL")
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

// --- Projects ---

func (s *PostgresStore) CreateProject(ctx context.Context, p *Project) error {
	meta, _ := json.Marshal(p.Metadata)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO projects (id, name, root_path, metadata)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET name=$2, root_path=$3, metadata=$4, updated_at=now()`,
		p.ID, p.Name, p.RootPath, meta)
	return err
}

func (s *PostgresStore) GetProject(ctx context.Context, id string) (*Project, error) {
	p := &Project{}
	var meta []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, root_path, metadata, created_at, updated_at FROM projects WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &p.RootPath, &meta, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(meta, &p.Metadata)
	return p, nil
}

func (s *PostgresStore) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, root_path, metadata, created_at, updated_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []Project
	for rows.Next() {
		var p Project
		var meta []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.RootPath, &meta, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(meta, &p.Metadata)
		projects = append(projects, p)
	}
	return projects, nil
}

// --- Memories ---

func (s *PostgresStore) SetMemory(ctx context.Context, m *Memory, embedding Vector) error {
	var embStr *string
	if embedding != nil {
		es := vectorToString(embedding)
		embStr = &es
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO memories (project_id, topic, key, value, embedding, created_by)
		 VALUES ($1, $2, $3, $4, $5::vector, $6)
		 ON CONFLICT (project_id, topic, key) DO UPDATE
		 SET value=$4, embedding=COALESCE($5::vector, memories.embedding), updated_at=now()`,
		m.ProjectID, m.Topic, m.Key, m.Value, embStr, m.CreatedBy)
	return err
}

func (s *PostgresStore) GetMemory(ctx context.Context, projectID, topic, key string) (*Memory, error) {
	m := &Memory{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, topic, key, value, created_at, updated_at, created_by
		 FROM memories WHERE project_id=$1 AND topic=$2 AND key=$3`,
		projectID, topic, key).
		Scan(&m.ID, &m.ProjectID, &m.Topic, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *PostgresStore) ListMemories(ctx context.Context, projectID, topic string) ([]Memory, error) {
	query := `SELECT id, project_id, topic, key, value, created_at, updated_at, created_by
		 FROM memories WHERE project_id=$1`
	args := []any{projectID}
	if topic != "" {
		query += ` AND topic=$2`
		args = append(args, topic)
	}
	query += ` ORDER BY topic, key`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Topic, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, nil
}

func (s *PostgresStore) DeleteMemory(ctx context.Context, projectID, topic, key string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM memories WHERE project_id=$1 AND topic=$2 AND key=$3`,
		projectID, topic, key)
	return err
}

func (s *PostgresStore) SearchMemories(ctx context.Context, projectID string, query string, embedding Vector, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}

	// Semantic search if embedding provided, otherwise full-text search
	var sqlQuery string
	var args []any

	if embedding != nil {
		embStr := vectorToString(embedding)
		sqlQuery = `SELECT id, project_id, topic, key, value, created_at, updated_at, created_by,
			    1 - (embedding <=> $2::vector) AS score
			    FROM memories
			    WHERE project_id=$1 AND embedding IS NOT NULL
			    ORDER BY embedding <=> $2::vector
			    LIMIT $3`
		args = []any{projectID, embStr, limit}
	} else {
		sqlQuery = `SELECT id, project_id, topic, key, value, created_at, updated_at, created_by,
			    ts_rank(to_tsvector('english', value), websearch_to_tsquery('english', $2)) AS score
			    FROM memories
			    WHERE project_id=$1 AND to_tsvector('english', value) @@ websearch_to_tsquery('english', $2)
			    ORDER BY score DESC
			    LIMIT $3`
		args = []any{projectID, query, limit}
	}

	rows, err := s.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Topic, &m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.Score); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, nil
}

// --- Sessions ---

func (s *PostgresStore) CreateSession(ctx context.Context, sess *Session, embedding Vector) error {
	meta, _ := json.Marshal(sess.Metadata)
	var embStr *string
	if embedding != nil {
		es := vectorToString(embedding)
		embStr = &es
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (project_id, session_num, title, summary, content, embedding, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6::vector, $7)
		 ON CONFLICT (project_id, session_num) DO UPDATE
		 SET title=$3, summary=$4, content=$5, embedding=COALESCE($6::vector, sessions.embedding), metadata=$7`,
		sess.ProjectID, sess.SessionNum, sess.Title, sess.Summary, sess.Content, embStr, meta)
	return err
}

func (s *PostgresStore) GetSession(ctx context.Context, projectID string, sessionNum int) (*Session, error) {
	sess := &Session{}
	var meta []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, session_num, title, summary, content, metadata, created_at
		 FROM sessions WHERE project_id=$1 AND session_num=$2`,
		projectID, sessionNum).
		Scan(&sess.ID, &sess.ProjectID, &sess.SessionNum, &sess.Title, &sess.Summary, &sess.Content, &meta, &sess.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(meta, &sess.Metadata)
	return sess, nil
}

func (s *PostgresStore) ListSessions(ctx context.Context, projectID string) ([]Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, project_id, session_num, title, summary, metadata, created_at
		 FROM sessions WHERE project_id=$1 ORDER BY session_num`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var sess Session
		var meta []byte
		if err := rows.Scan(&sess.ID, &sess.ProjectID, &sess.SessionNum, &sess.Title, &sess.Summary, &meta, &sess.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(meta, &sess.Metadata)
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (s *PostgresStore) SearchSessions(ctx context.Context, projectID string, query string, embedding Vector, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 10
	}

	var sqlQuery string
	var args []any

	if embedding != nil {
		embStr := vectorToString(embedding)
		sqlQuery = `SELECT id, project_id, session_num, title, summary, metadata, created_at,
			    1 - (embedding <=> $2::vector) AS score
			    FROM sessions
			    WHERE project_id=$1 AND embedding IS NOT NULL
			    ORDER BY embedding <=> $2::vector
			    LIMIT $3`
		args = []any{projectID, embStr, limit}
	} else {
		sqlQuery = `SELECT id, project_id, session_num, title, summary, metadata, created_at,
			    ts_rank(to_tsvector('english', coalesce(title,'') || ' ' || coalesce(summary,'') || ' ' || coalesce(content,'')),
			    websearch_to_tsquery('english', $2)) AS score
			    FROM sessions
			    WHERE project_id=$1
			    AND to_tsvector('english', coalesce(title,'') || ' ' || coalesce(summary,'') || ' ' || coalesce(content,''))
			    @@ websearch_to_tsquery('english', $2)
			    ORDER BY score DESC
			    LIMIT $3`
		args = []any{projectID, query, limit}
	}

	rows, err := s.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		slog.Error("session search query failed", "error", err)
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var sess Session
		var meta []byte
		if err := rows.Scan(&sess.ID, &sess.ProjectID, &sess.SessionNum, &sess.Title, &sess.Summary, &meta, &sess.CreatedAt, &sess.Score); err != nil {
			return nil, err
		}
		json.Unmarshal(meta, &sess.Metadata)
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// --- File Index ---

func (s *PostgresStore) IndexFile(ctx context.Context, f *FileEntry, embedding Vector) error {
	symbols, _ := json.Marshal(f.Symbols)
	var embStr *string
	if embedding != nil {
		es := vectorToString(embedding)
		embStr = &es
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO file_index (project_id, file_path, file_type, symbols, summary, embedding)
		 VALUES ($1, $2, $3, $4, $5, $6::vector)
		 ON CONFLICT (project_id, file_path) DO UPDATE
		 SET file_type=$3, symbols=$4, summary=$5, embedding=COALESCE($6::vector, file_index.embedding), last_indexed=now()`,
		f.ProjectID, f.FilePath, f.FileType, symbols, f.Summary, embStr)
	return err
}

func (s *PostgresStore) SearchFiles(ctx context.Context, projectID string, query string, embedding Vector, limit int) ([]FileEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	var sqlQuery string
	var args []any

	if embedding != nil {
		embStr := vectorToString(embedding)
		sqlQuery = `SELECT id, project_id, file_path, file_type, symbols, summary, last_indexed,
			    1 - (embedding <=> $2::vector) AS score
			    FROM file_index
			    WHERE project_id=$1 AND embedding IS NOT NULL
			    ORDER BY embedding <=> $2::vector
			    LIMIT $3`
		args = []any{projectID, embStr, limit}
	} else {
		sqlQuery = `SELECT id, project_id, file_path, file_type, symbols, summary, last_indexed,
			    ts_rank(to_tsvector('english', coalesce(summary,'')), websearch_to_tsquery('english', $2)) AS score
			    FROM file_index
			    WHERE project_id=$1
			    AND to_tsvector('english', coalesce(summary,'')) @@ websearch_to_tsquery('english', $2)
			    ORDER BY score DESC
			    LIMIT $3`
		args = []any{projectID, query, limit}
	}

	rows, err := s.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []FileEntry
	for rows.Next() {
		var f FileEntry
		var symbols []byte
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.FilePath, &f.FileType, &symbols, &f.Summary, &f.LastIndexed, &f.Score); err != nil {
			return nil, err
		}
		json.Unmarshal(symbols, &f.Symbols)
		files = append(files, f)
	}
	return files, nil
}

// --- Usage & Dashboard ---

func (s *PostgresStore) RecordUsage(ctx context.Context, u *UsageStat) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage_stats (project_id, tool_name, query_text, results_count, tokens_estimated)
		 VALUES ($1, $2, $3, $4, $5)`,
		u.ProjectID, u.ToolName, u.QueryText, u.ResultsCount, u.TokensEstimated)
	return err
}

func (s *PostgresStore) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	ds := &DashboardStats{}

	// Count projects, memories, sessions, files
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&ds.ProjectCount)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM memories`).Scan(&ds.MemoryCount)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&ds.SessionCount)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM file_index`).Scan(&ds.FileCount)

	// Total usage stats
	_ = s.pool.QueryRow(ctx,
		`SELECT coalesce(count(*),0), coalesce(sum(tokens_estimated),0) FROM usage_stats`).
		Scan(&ds.TotalQueries, &ds.TotalTokensSaved)

	// Last 24h
	_ = s.pool.QueryRow(ctx,
		`SELECT coalesce(count(*),0), coalesce(sum(tokens_estimated),0) FROM usage_stats WHERE created_at > now() - interval '24 hours'`).
		Scan(&ds.QueriesLast24h, &ds.TokensLast24h)

	// Per-project stats
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return ds, err
	}
	for _, p := range projects {
		ps, err := s.GetProjectStats(ctx, p.ID)
		if err != nil {
			continue
		}
		ds.Projects = append(ds.Projects, *ps)
	}

	return ds, nil
}

func (s *PostgresStore) GetProjectStats(ctx context.Context, projectID string) (*ProjectStats, error) {
	p, err := s.GetProject(ctx, projectID)
	if err != nil || p == nil {
		return nil, err
	}

	ps := &ProjectStats{Project: *p}
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM memories WHERE project_id=$1`, projectID).Scan(&ps.MemoryCount)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE project_id=$1`, projectID).Scan(&ps.SessionCount)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM file_index WHERE project_id=$1`, projectID).Scan(&ps.FileCount)
	_ = s.pool.QueryRow(ctx,
		`SELECT coalesce(count(*),0), coalesce(sum(tokens_estimated),0) FROM usage_stats WHERE project_id=$1`,
		projectID).Scan(&ps.QueryCount, &ps.TokensSaved)

	return ps, nil
}

func (s *PostgresStore) SearchAll(ctx context.Context, query string, embedding Vector, limit int) (*SearchAllResult, error) {
	if limit <= 0 {
		limit = 10
	}

	result := &SearchAllResult{}

	// Get all projects to search across
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return result, err
	}

	for _, p := range projects {
		memories, err := s.SearchMemories(ctx, p.ID, query, embedding, limit)
		if err == nil {
			result.Memories = append(result.Memories, memories...)
		}
		sessions, err := s.SearchSessions(ctx, p.ID, query, embedding, limit)
		if err == nil {
			result.Sessions = append(result.Sessions, sessions...)
		}
		files, err := s.SearchFiles(ctx, p.ID, query, embedding, limit)
		if err == nil {
			result.Files = append(result.Files, files...)
		}
	}

	// Sort each slice by score descending and cap at limit
	sortAndCap := func(n int) int {
		if n > limit {
			return limit
		}
		return n
	}

	// Sort memories by score desc
	for i := 0; i < len(result.Memories); i++ {
		for j := i + 1; j < len(result.Memories); j++ {
			if result.Memories[j].Score > result.Memories[i].Score {
				result.Memories[i], result.Memories[j] = result.Memories[j], result.Memories[i]
			}
		}
	}
	result.Memories = result.Memories[:sortAndCap(len(result.Memories))]

	// Sort sessions by score desc
	for i := 0; i < len(result.Sessions); i++ {
		for j := i + 1; j < len(result.Sessions); j++ {
			if result.Sessions[j].Score > result.Sessions[i].Score {
				result.Sessions[i], result.Sessions[j] = result.Sessions[j], result.Sessions[i]
			}
		}
	}
	result.Sessions = result.Sessions[:sortAndCap(len(result.Sessions))]

	// Sort files by score desc
	for i := 0; i < len(result.Files); i++ {
		for j := i + 1; j < len(result.Files); j++ {
			if result.Files[j].Score > result.Files[i].Score {
				result.Files[i], result.Files[j] = result.Files[j], result.Files[i]
			}
		}
	}
	result.Files = result.Files[:sortAndCap(len(result.Files))]

	return result, nil
}

// vectorToString formats a float32 slice as a pgvector literal: "[0.1,0.2,0.3]"
func vectorToString(v Vector) string {
	if len(v) == 0 {
		return "[]"
	}
	buf := make([]byte, 0, len(v)*8)
	buf = append(buf, '[')
	for i, f := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Sprintf("%g", f)...)
	}
	buf = append(buf, ']')
	return string(buf)
}
