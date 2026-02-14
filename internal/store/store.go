package store

import (
	"context"
	"time"
)

// Vector is a float32 slice representing an embedding.
type Vector = []float32

// Project represents a registered project.
type Project struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	RootPath  string            `json:"root_path,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Memory represents a key-value memory entry with optional embedding.
type Memory struct {
	ID        int64     `json:"id"`
	ProjectID string    `json:"project_id"`
	Topic     string    `json:"topic"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	Score     float64   `json:"score,omitempty"` // similarity score for search results
}

// Session represents a session transcript.
type Session struct {
	ID         int64          `json:"id"`
	ProjectID  string         `json:"project_id"`
	SessionNum int            `json:"session_num"`
	Title      string         `json:"title"`
	Summary    string         `json:"summary,omitempty"`
	Content    string         `json:"content,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	Score      float64        `json:"score,omitempty"`
}

// FileEntry represents an indexed file.
type FileEntry struct {
	ID          int64     `json:"id"`
	ProjectID   string    `json:"project_id"`
	FilePath    string    `json:"file_path"`
	FileType    string    `json:"file_type,omitempty"`
	Symbols     []any     `json:"symbols,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	LastIndexed time.Time `json:"last_indexed"`
	Score       float64   `json:"score,omitempty"`
}

// UsageStat records a single tool invocation for analytics.
type UsageStat struct {
	ID              int64     `json:"id"`
	ProjectID       string    `json:"project_id"`
	ToolName        string    `json:"tool_name"`
	QueryText       string    `json:"query_text"`
	ResultsCount    int       `json:"results_count"`
	TokensEstimated int       `json:"tokens_estimated"`
	CreatedAt       time.Time `json:"created_at"`
}

// DashboardStats aggregates counts across all projects.
type DashboardStats struct {
	ProjectCount     int
	MemoryCount      int
	SessionCount     int
	FileCount        int
	TotalQueries     int
	TotalTokensSaved int
	QueriesLast24h   int
	TokensLast24h    int
	EmbeddingStatus  string
	Projects         []ProjectStats
}

// ProjectStats aggregates counts for a single project.
type ProjectStats struct {
	Project      Project
	MemoryCount  int
	SessionCount int
	FileCount    int
	QueryCount   int
	TokensSaved  int
}

// SearchAllResult holds cross-entity search results.
type SearchAllResult struct {
	Memories []Memory
	Sessions []Session
	Files    []FileEntry
}

// Store defines the persistence interface.
type Store interface {
	// Projects
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)

	// Memories
	SetMemory(ctx context.Context, m *Memory, embedding Vector) error
	GetMemory(ctx context.Context, projectID, topic, key string) (*Memory, error)
	ListMemories(ctx context.Context, projectID, topic string) ([]Memory, error)
	DeleteMemory(ctx context.Context, projectID, topic, key string) error
	SearchMemories(ctx context.Context, projectID string, query string, embedding Vector, limit int) ([]Memory, error)

	// Sessions
	CreateSession(ctx context.Context, s *Session, embedding Vector) error
	GetSession(ctx context.Context, projectID string, sessionNum int) (*Session, error)
	ListSessions(ctx context.Context, projectID string) ([]Session, error)
	SearchSessions(ctx context.Context, projectID string, query string, embedding Vector, limit int) ([]Session, error)

	// File Index
	IndexFile(ctx context.Context, f *FileEntry, embedding Vector) error
	SearchFiles(ctx context.Context, projectID string, query string, embedding Vector, limit int) ([]FileEntry, error)

	// Usage & Dashboard
	RecordUsage(ctx context.Context, u *UsageStat) error
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetProjectStats(ctx context.Context, projectID string) (*ProjectStats, error)
	SearchAll(ctx context.Context, query string, embedding Vector, limit int) (*SearchAllResult, error)

	// Lifecycle
	Close()
}
