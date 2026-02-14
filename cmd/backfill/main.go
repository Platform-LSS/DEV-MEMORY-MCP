// Backfill loads existing project knowledge into DevMemory.
// Usage: go run ./cmd/backfill --project-id=plss-fhir --root=/path/to/project
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Platform-LSS/devmemory/internal/embedding"
	"github.com/Platform-LSS/devmemory/internal/store"
)

func main() {
	projectID := flag.String("project-id", "plss-fhir", "Project ID")
	projectName := flag.String("project-name", "PLSS FHIR Server", "Project display name")
	rootPath := flag.String("root", "", "Project root path")
	dbURL := flag.String("db", "", "Database URL (or DATABASE_URL env)")
	embURL := flag.String("embed-url", "", "Embedding URL (or EMBEDDING_URL env)")
	flag.Parse()

	if *rootPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --root is required")
		os.Exit(1)
	}

	if *dbURL == "" {
		*dbURL = os.Getenv("DATABASE_URL")
	}
	if *dbURL == "" {
		*dbURL = "postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable"
	}
	if *embURL == "" {
		*embURL = os.Getenv("EMBEDDING_URL")
	}
	if *embURL == "" {
		*embURL = "http://localhost:8091/embed"
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()

	pgStore, err := store.NewPostgresStore(ctx, *dbURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pgStore.Close()

	emb := embedding.New(*embURL, 384)
	slog.Info("embedding", "status", emb.Status())

	// Register project
	if err := pgStore.CreateProject(ctx, &store.Project{
		ID:       *projectID,
		Name:     *projectName,
		RootPath: *rootPath,
	}); err != nil {
		slog.Error("register project", "error", err)
		os.Exit(1)
	}
	slog.Info("project registered", "id", *projectID)

	var total int

	// --- Load spec files as memories (topic: "spec") ---
	specDir := filepath.Join(*rootPath, "spec")
	total += loadDirAsMemories(ctx, pgStore, emb, *projectID, specDir, "spec")

	// --- Load doc files as memories (topic: "docs") ---
	docsDir := filepath.Join(*rootPath, "docs")
	total += loadDirAsMemories(ctx, pgStore, emb, *projectID, docsDir, "docs")

	// --- Load ADR files as memories (topic: "adr") ---
	adrDir := filepath.Join(*rootPath, "docs", "adr")
	total += loadDirAsMemories(ctx, pgStore, emb, *projectID, adrDir, "adr")

	// --- Load CLAUDE.md as memory ---
	total += loadFileAsMemory(ctx, pgStore, emb, *projectID, filepath.Join(*rootPath, "CLAUDE.md"), "project", "claude-md")

	// --- Load README.md as memory ---
	total += loadFileAsMemory(ctx, pgStore, emb, *projectID, filepath.Join(*rootPath, "README.md"), "project", "readme")

	// --- Load key lessons from auto-memory ---
	memoryFile := filepath.Join(os.Getenv("HOME"), ".claude/projects/-Users-eamonstafford-PLSS-Projects-plss-fhir-server/memory/MEMORY.md")
	total += loadFileAsMemory(ctx, pgStore, emb, *projectID, memoryFile, "lessons", "project-memory")

	// --- Load transcripts as sessions ---
	transcriptDir := filepath.Join(*rootPath, "transcripts")
	total += loadTranscriptsAsSessions(ctx, pgStore, emb, *projectID, transcriptDir)

	// --- Load phase reports as sessions ---
	phaseDir := filepath.Join(*rootPath, "transcripts", "phases")
	total += loadTranscriptsAsSessions(ctx, pgStore, emb, *projectID, phaseDir)

	// --- Load transcript index as memory ---
	total += loadFileAsMemory(ctx, pgStore, emb, *projectID, filepath.Join(transcriptDir, "INDEX.md"), "project", "transcript-index")

	// --- Index Go source files ---
	total += indexGoFiles(ctx, pgStore, emb, *projectID, *rootPath)

	slog.Info("backfill complete", "total_items", total, "project", *projectID)
}

func loadDirAsMemories(ctx context.Context, s store.Store, emb *embedding.Service, projectID, dir, topic string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("skip dir", "dir", dir, "error", err)
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skip file", "path", path, "error", err)
			continue
		}

		key := strings.TrimSuffix(e.Name(), ".md")
		value := string(content)

		// For embedding, use first 500 chars as summary (embedding has 128 token limit)
		embText := value
		if len(embText) > 2000 {
			embText = embText[:2000]
		}
		vec := emb.Embed(ctx, embText)

		if err := s.SetMemory(ctx, &store.Memory{
			ProjectID: projectID,
			Topic:     topic,
			Key:       key,
			Value:     value,
		}, vec); err != nil {
			slog.Error("set memory", "topic", topic, "key", key, "error", err)
			continue
		}
		slog.Info("loaded memory", "topic", topic, "key", key, "size", len(value))
		count++
	}
	return count
}

func loadFileAsMemory(ctx context.Context, s store.Store, emb *embedding.Service, projectID, path, topic, key string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("skip file", "path", path, "error", err)
		return 0
	}
	value := string(content)
	embText := value
	if len(embText) > 2000 {
		embText = embText[:2000]
	}
	vec := emb.Embed(ctx, embText)

	if err := s.SetMemory(ctx, &store.Memory{
		ProjectID: projectID,
		Topic:     topic,
		Key:       key,
		Value:     value,
	}, vec); err != nil {
		slog.Error("set memory", "topic", topic, "key", key, "error", err)
		return 0
	}
	slog.Info("loaded memory", "topic", topic, "key", key, "size", len(value))
	return 1
}

func loadTranscriptsAsSessions(ctx context.Context, s store.Store, emb *embedding.Service, projectID, dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("skip dir", "dir", dir, "error", err)
		return 0
	}
	count := 0
	sessionNum := 100 // Start at 100 to avoid conflicts with any existing sessions

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == "INDEX.md" {
			continue // Loaded separately as memory
		}

		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skip transcript", "path", path, "error", err)
			continue
		}

		title := strings.TrimSuffix(e.Name(), ".md")
		value := string(content)

		// Extract first paragraph as summary
		summary := extractSummary(value)

		embText := summary
		if embText == "" {
			embText = title
		}
		vec := emb.Embed(ctx, embText)

		if err := s.CreateSession(ctx, &store.Session{
			ProjectID:  projectID,
			SessionNum: sessionNum,
			Title:      title,
			Summary:    summary,
			Content:    value,
		}, vec); err != nil {
			slog.Error("create session", "title", title, "error", err)
			continue
		}
		slog.Info("loaded session", "num", sessionNum, "title", title, "size", len(value))
		sessionNum++
		count++
	}
	return count
}

func indexGoFiles(ctx context.Context, s store.Store, emb *embedding.Service, projectID, rootPath string) int {
	count := 0
	filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(rootPath, path)
		summary := extractGoSummary(string(content))

		vec := emb.Embed(ctx, summary)

		if err := s.IndexFile(ctx, &store.FileEntry{
			ProjectID: projectID,
			FilePath:  relPath,
			FileType:  "go",
			Summary:   summary,
		}, vec); err != nil {
			slog.Warn("index file", "path", relPath, "error", err)
			return nil
		}
		slog.Info("indexed file", "path", relPath)
		count++
		return nil
	})
	return count
}

func extractSummary(content string) string {
	lines := strings.Split(content, "\n")
	var summary []string
	inContent := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && inContent {
			break // End of first paragraph
		}
		if strings.HasPrefix(trimmed, "#") {
			continue // Skip headers
		}
		if trimmed != "" {
			inContent = true
			summary = append(summary, trimmed)
		}
	}
	result := strings.Join(summary, " ")
	if len(result) > 500 {
		result = result[:500]
	}
	return result
}

func extractGoSummary(content string) string {
	lines := strings.Split(content, "\n")
	var parts []string

	// Collect package doc comment + function/type names
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// ") {
			parts = append(parts, strings.TrimPrefix(trimmed, "// "))
		}
		if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") {
			// Extract just the signature
			if idx := strings.Index(trimmed, "{"); idx > 0 {
				parts = append(parts, strings.TrimSpace(trimmed[:idx]))
			} else {
				parts = append(parts, trimmed)
			}
		}
	}

	result := strings.Join(parts, ". ")
	if len(result) > 1000 {
		result = result[:1000]
	}
	return result
}
