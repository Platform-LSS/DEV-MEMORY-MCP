package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/Platform-LSS/devmemory/internal/embedding"
	"github.com/Platform-LSS/devmemory/internal/store"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// EventPublisher is satisfied by web.EventBus. Optional — nil when not in web transport.
type EventPublisher interface {
	Publish(event string)
}

// Server wraps the MCP server with our store and embedding service.
type Server struct {
	mcp       *server.MCPServer
	store     store.Store
	embedding *embedding.Service
	events    EventPublisher
}

// New creates a new MCP server with all tools registered.
func New(s store.Store, emb *embedding.Service) *Server {
	srv := &Server{
		store:     s,
		embedding: emb,
	}

	srv.mcp = server.NewMCPServer(
		"devmemory",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	srv.registerTools()
	return srv
}

// SetEvents wires an optional event publisher for real-time dashboard updates.
func (s *Server) SetEvents(ep EventPublisher) {
	s.events = ep
}

// MCPServer returns the underlying MCP server for transport binding.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcp
}

// tokenEstimate returns a heuristic token count for a tool call.
func tokenEstimate(toolName string, resultsCount int) int {
	switch toolName {
	case "memory_search":
		return resultsCount * 500
	case "session_search":
		return resultsCount * 2000
	case "file_search":
		return resultsCount * 800
	default:
		return 100
	}
}

// recordUsage logs a tool invocation and publishes an SSE event.
func (s *Server) recordUsage(ctx context.Context, toolName, projectID, query string, resultsCount int) {
	tokens := tokenEstimate(toolName, resultsCount)
	if err := s.store.RecordUsage(ctx, &store.UsageStat{
		ProjectID:       projectID,
		ToolName:        toolName,
		QueryText:       query,
		ResultsCount:    resultsCount,
		TokensEstimated: tokens,
	}); err != nil {
		slog.Warn("record usage", "error", err)
	}
	if s.events != nil {
		s.events.Publish("dashboard-stats")
	}
}

func (s *Server) registerTools() {
	// --- Project tools ---
	s.mcp.AddTool(
		mcpsdk.NewTool("project_register",
			mcpsdk.WithDescription("Register a project for memory tracking"),
			mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Unique project identifier (slug)")),
			mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Human-readable project name")),
			mcpsdk.WithString("root_path", mcpsdk.Description("Filesystem root path of the project")),
		),
		s.handleProjectRegister,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("project_list",
			mcpsdk.WithDescription("List all registered projects"),
		),
		s.handleProjectList,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("project_status",
			mcpsdk.WithDescription("Get project status: session count, memory count, embedding status"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
		),
		s.handleProjectStatus,
	)

	// --- Memory tools ---
	s.mcp.AddTool(
		mcpsdk.NewTool("memory_set",
			mcpsdk.WithDescription("Store or update a memory entry. Generates embedding for semantic search."),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("topic", mcpsdk.Required(), mcpsdk.Description("Memory topic (e.g. 'architecture', 'lesson', 'preference')")),
			mcpsdk.WithString("key", mcpsdk.Required(), mcpsdk.Description("Memory key within topic")),
			mcpsdk.WithString("value", mcpsdk.Required(), mcpsdk.Description("Memory value (text content)")),
		),
		s.handleMemorySet,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("memory_get",
			mcpsdk.WithDescription("Get a specific memory by topic and key"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("topic", mcpsdk.Required(), mcpsdk.Description("Memory topic")),
			mcpsdk.WithString("key", mcpsdk.Required(), mcpsdk.Description("Memory key")),
		),
		s.handleMemoryGet,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("memory_list",
			mcpsdk.WithDescription("List memories for a project, optionally filtered by topic"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("topic", mcpsdk.Description("Filter by topic (optional)")),
		),
		s.handleMemoryList,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("memory_search",
			mcpsdk.WithDescription("Semantic search over project memories. Uses vector similarity if embeddings are enabled, otherwise full-text search."),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query text")),
			mcpsdk.WithString("limit", mcpsdk.Description("Max results (default 10)")),
		),
		s.handleMemorySearch,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("memory_delete",
			mcpsdk.WithDescription("Delete a specific memory entry"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("topic", mcpsdk.Required(), mcpsdk.Description("Memory topic")),
			mcpsdk.WithString("key", mcpsdk.Required(), mcpsdk.Description("Memory key")),
		),
		s.handleMemoryDelete,
	)

	// --- Session tools ---
	s.mcp.AddTool(
		mcpsdk.NewTool("session_create",
			mcpsdk.WithDescription("Create or update a session transcript. Generates embedding from summary for semantic search."),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("session_num", mcpsdk.Required(), mcpsdk.Description("Session number (integer)")),
			mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Session title")),
			mcpsdk.WithString("summary", mcpsdk.Description("Session summary (used for embedding)")),
			mcpsdk.WithString("content", mcpsdk.Description("Full session content/transcript")),
		),
		s.handleSessionCreate,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("session_get",
			mcpsdk.WithDescription("Get a specific session by number"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("session_num", mcpsdk.Required(), mcpsdk.Description("Session number")),
		),
		s.handleSessionGet,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("session_list",
			mcpsdk.WithDescription("List all sessions for a project"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
		),
		s.handleSessionList,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("session_search",
			mcpsdk.WithDescription("Semantic search over session transcripts"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query text")),
			mcpsdk.WithString("limit", mcpsdk.Description("Max results (default 10)")),
		),
		s.handleSessionSearch,
	)

	// --- File index tools ---
	s.mcp.AddTool(
		mcpsdk.NewTool("file_index",
			mcpsdk.WithDescription("Index a project file with metadata and summary for semantic search"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("file_path", mcpsdk.Required(), mcpsdk.Description("File path relative to project root")),
			mcpsdk.WithString("file_type", mcpsdk.Description("File type (e.g. 'go', 'sql', 'md')")),
			mcpsdk.WithString("summary", mcpsdk.Description("File summary (used for embedding)")),
			mcpsdk.WithString("symbols", mcpsdk.Description("JSON array of symbols (functions, types, etc.)")),
		),
		s.handleFileIndex,
	)

	s.mcp.AddTool(
		mcpsdk.NewTool("file_search",
			mcpsdk.WithDescription("Semantic search over indexed project files"),
			mcpsdk.WithString("project_id", mcpsdk.Required(), mcpsdk.Description("Project identifier")),
			mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query text")),
			mcpsdk.WithString("limit", mcpsdk.Description("Max results (default 10)")),
		),
		s.handleFileSearch,
	)
}

// --- Tool Handlers ---

func (s *Server) handleProjectRegister(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := stringArg(req, "id")
	name := stringArg(req, "name")
	rootPath := stringArg(req, "root_path")

	if id == "" || name == "" {
		return mcpsdk.NewToolResultError("id and name are required"), nil
	}

	err := s.store.CreateProject(ctx, &store.Project{
		ID:       id,
		Name:     name,
		RootPath: rootPath,
	})
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("create project: %v", err)), nil
	}
	s.recordUsage(ctx, "project_register", id, "", 1)
	return mcpsdk.NewToolResultText(fmt.Sprintf("Project '%s' registered (id=%s)", name, id)), nil
}

func (s *Server) handleProjectList(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list projects: %v", err)), nil
	}
	s.recordUsage(ctx, "project_list", "", "", len(projects))
	data, _ := json.MarshalIndent(projects, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleProjectStatus(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	if projectID == "" {
		return mcpsdk.NewToolResultError("project_id is required"), nil
	}

	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get project: %v", err)), nil
	}
	if p == nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("project '%s' not found", projectID)), nil
	}

	memories, _ := s.store.ListMemories(ctx, projectID, "")
	sessions, _ := s.store.ListSessions(ctx, projectID)

	status := map[string]any{
		"project":          p,
		"memory_count":     len(memories),
		"session_count":    len(sessions),
		"embedding_status": s.embedding.Status(),
	}
	s.recordUsage(ctx, "project_status", projectID, "", 1)
	data, _ := json.MarshalIndent(status, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemorySet(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	topic := stringArg(req, "topic")
	key := stringArg(req, "key")
	value := stringArg(req, "value")

	if projectID == "" || topic == "" || key == "" || value == "" {
		return mcpsdk.NewToolResultError("project_id, topic, key, and value are required"), nil
	}

	emb := s.embedding.Embed(ctx, value)
	err := s.store.SetMemory(ctx, &store.Memory{
		ProjectID: projectID,
		Topic:     topic,
		Key:       key,
		Value:     value,
	}, emb)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("set memory: %v", err)), nil
	}

	embedded := "no"
	if emb != nil {
		embedded = "yes"
	}
	s.recordUsage(ctx, "memory_set", projectID, topic+"/"+key, 1)
	return mcpsdk.NewToolResultText(fmt.Sprintf("Memory set: %s/%s (embedded: %s)", topic, key, embedded)), nil
}

func (s *Server) handleMemoryGet(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	topic := stringArg(req, "topic")
	key := stringArg(req, "key")

	m, err := s.store.GetMemory(ctx, projectID, topic, key)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get memory: %v", err)), nil
	}
	if m == nil {
		return mcpsdk.NewToolResultText("not found"), nil
	}
	s.recordUsage(ctx, "memory_get", projectID, topic+"/"+key, 1)
	data, _ := json.MarshalIndent(m, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemoryList(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	topic := stringArg(req, "topic")

	memories, err := s.store.ListMemories(ctx, projectID, topic)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list memories: %v", err)), nil
	}
	s.recordUsage(ctx, "memory_list", projectID, topic, len(memories))
	data, _ := json.MarshalIndent(memories, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemorySearch(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	query := stringArg(req, "query")
	limit := intArg(req, "limit", 10)

	if projectID == "" || query == "" {
		return mcpsdk.NewToolResultError("project_id and query are required"), nil
	}

	emb := s.embedding.Embed(ctx, query)
	results, err := s.store.SearchMemories(ctx, projectID, query, emb, limit)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("search memories: %v", err)), nil
	}

	searchType := "full-text"
	if emb != nil {
		searchType = "semantic (vector)"
	}
	response := map[string]any{
		"search_type": searchType,
		"query":       query,
		"count":       len(results),
		"results":     results,
	}
	s.recordUsage(ctx, "memory_search", projectID, query, len(results))
	data, _ := json.MarshalIndent(response, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleMemoryDelete(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	topic := stringArg(req, "topic")
	key := stringArg(req, "key")

	err := s.store.DeleteMemory(ctx, projectID, topic, key)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("delete memory: %v", err)), nil
	}
	s.recordUsage(ctx, "memory_delete", projectID, topic+"/"+key, 0)
	return mcpsdk.NewToolResultText(fmt.Sprintf("Deleted: %s/%s", topic, key)), nil
}

func (s *Server) handleSessionCreate(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	sessionNum := intArg(req, "session_num", 0)
	title := stringArg(req, "title")
	summary := stringArg(req, "summary")
	content := stringArg(req, "content")

	if projectID == "" || sessionNum == 0 || title == "" {
		return mcpsdk.NewToolResultError("project_id, session_num, and title are required"), nil
	}

	// Embed the summary for semantic search
	embText := summary
	if embText == "" {
		embText = title
	}
	emb := s.embedding.Embed(ctx, embText)

	err := s.store.CreateSession(ctx, &store.Session{
		ProjectID:  projectID,
		SessionNum: sessionNum,
		Title:      title,
		Summary:    summary,
		Content:    content,
	}, emb)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("create session: %v", err)), nil
	}
	s.recordUsage(ctx, "session_create", projectID, title, 1)
	return mcpsdk.NewToolResultText(fmt.Sprintf("Session %d created: %s", sessionNum, title)), nil
}

func (s *Server) handleSessionGet(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	sessionNum := intArg(req, "session_num", 0)

	sess, err := s.store.GetSession(ctx, projectID, sessionNum)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("get session: %v", err)), nil
	}
	if sess == nil {
		return mcpsdk.NewToolResultText("not found"), nil
	}
	s.recordUsage(ctx, "session_get", projectID, "", 1)
	data, _ := json.MarshalIndent(sess, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleSessionList(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")

	sessions, err := s.store.ListSessions(ctx, projectID)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("list sessions: %v", err)), nil
	}
	s.recordUsage(ctx, "session_list", projectID, "", len(sessions))
	data, _ := json.MarshalIndent(sessions, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleSessionSearch(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	query := stringArg(req, "query")
	limit := intArg(req, "limit", 10)

	if projectID == "" || query == "" {
		return mcpsdk.NewToolResultError("project_id and query are required"), nil
	}

	emb := s.embedding.Embed(ctx, query)
	results, err := s.store.SearchSessions(ctx, projectID, query, emb, limit)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("search sessions: %v", err)), nil
	}

	searchType := "full-text"
	if emb != nil {
		searchType = "semantic (vector)"
	}
	response := map[string]any{
		"search_type": searchType,
		"query":       query,
		"count":       len(results),
		"results":     results,
	}
	s.recordUsage(ctx, "session_search", projectID, query, len(results))
	data, _ := json.MarshalIndent(response, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

func (s *Server) handleFileIndex(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	filePath := stringArg(req, "file_path")
	fileType := stringArg(req, "file_type")
	summary := stringArg(req, "summary")
	symbolsStr := stringArg(req, "symbols")

	if projectID == "" || filePath == "" {
		return mcpsdk.NewToolResultError("project_id and file_path are required"), nil
	}

	var symbols []any
	if symbolsStr != "" {
		json.Unmarshal([]byte(symbolsStr), &symbols)
	}

	emb := s.embedding.Embed(ctx, summary)
	err := s.store.IndexFile(ctx, &store.FileEntry{
		ProjectID: projectID,
		FilePath:  filePath,
		FileType:  fileType,
		Summary:   summary,
		Symbols:   symbols,
	}, emb)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("index file: %v", err)), nil
	}
	s.recordUsage(ctx, "file_index", projectID, filePath, 1)
	return mcpsdk.NewToolResultText(fmt.Sprintf("Indexed: %s", filePath)), nil
}

func (s *Server) handleFileSearch(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	projectID := stringArg(req, "project_id")
	query := stringArg(req, "query")
	limit := intArg(req, "limit", 10)

	if projectID == "" || query == "" {
		return mcpsdk.NewToolResultError("project_id and query are required"), nil
	}

	emb := s.embedding.Embed(ctx, query)
	results, err := s.store.SearchFiles(ctx, projectID, query, emb, limit)
	if err != nil {
		return mcpsdk.NewToolResultError(fmt.Sprintf("search files: %v", err)), nil
	}

	searchType := "full-text"
	if emb != nil {
		searchType = "semantic (vector)"
	}
	response := map[string]any{
		"search_type": searchType,
		"query":       query,
		"count":       len(results),
		"results":     results,
	}
	s.recordUsage(ctx, "file_search", projectID, query, len(results))
	data, _ := json.MarshalIndent(response, "", "  ")
	return mcpsdk.NewToolResultText(string(data)), nil
}

// --- Helpers ---

func stringArg(req mcpsdk.CallToolRequest, name string) string {
	v, ok := req.Params.Arguments[name]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func intArg(req mcpsdk.CallToolRequest, name string, defaultVal int) int {
	v := stringArg(req, name)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid int arg", "name", name, "value", v)
		return defaultVal
	}
	return n
}
