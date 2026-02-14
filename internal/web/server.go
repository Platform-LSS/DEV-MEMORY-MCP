package web

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Platform-LSS/devmemory/internal/embedding"
	"github.com/Platform-LSS/devmemory/internal/store"
)

// WebServer serves the GOTH-stack dashboard.
type WebServer struct {
	store     store.Store
	embedding *embedding.Service
	events    *EventBus
	tmpl      *pageTemplates
}

// New creates a WebServer with parsed templates.
func New(s store.Store, emb *embedding.Service) (*WebServer, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &WebServer{
		store:     s,
		embedding: emb,
		events:    NewEventBus(),
		tmpl:      tmpl,
	}, nil
}

// Events returns the event bus for use by MCP tool handlers.
func (ws *WebServer) Events() *EventBus {
	return ws.events
}

// Routes returns the HTTP handler with all routes registered.
func (ws *WebServer) Routes() http.Handler {
	mux := http.NewServeMux()

	// Full pages
	mux.HandleFunc("GET /", ws.handleDashboard)
	mux.HandleFunc("GET /history", ws.handleHistory)
	mux.HandleFunc("GET /search", ws.handleSearch)
	mux.HandleFunc("GET /memories", ws.handleMemories)

	// HTMX partials
	mux.HandleFunc("GET /api/stats", ws.handleAPIStats)
	mux.HandleFunc("GET /api/cost", ws.handleAPICost)
	mux.HandleFunc("GET /api/projects", ws.handleAPIProjects)
	mux.HandleFunc("GET /api/history/sessions", ws.handleAPISessions)
	mux.HandleFunc("GET /api/history/detail", ws.handleAPISessionDetail)
	mux.HandleFunc("GET /api/search", ws.handleAPISearch)
	mux.HandleFunc("GET /api/memories", ws.handleAPIMemories)
	mux.HandleFunc("GET /api/memories/edit/{id}", ws.handleAPIMemoryEdit)
	mux.HandleFunc("PUT /api/memories/{id}", ws.handleAPIMemoryUpdate)
	mux.HandleFunc("DELETE /api/memories/{id}", ws.handleAPIMemoryDelete)
	mux.HandleFunc("POST /api/memories", ws.handleAPIMemoryCreate)

	return requestLogger(mux)
}

// --- Full Page Handlers ---

func (ws *WebServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	stats, err := ws.store.GetDashboardStats(r.Context())
	if err != nil {
		slog.Error("dashboard stats", "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	stats.EmbeddingStatus = ws.embedding.Status()
	ws.renderPage(w, "dashboard.html", map[string]any{
		"Stats":  stats,
		"Active": "dashboard",
		"Period": "24h",
	})
}

func (ws *WebServer) handleHistory(w http.ResponseWriter, r *http.Request) {
	projects, _ := ws.store.ListProjects(r.Context())
	ws.renderPage(w, "history.html", map[string]any{
		"Projects": projects,
		"Active":   "history",
	})
}

func (ws *WebServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	projects, _ := ws.store.ListProjects(r.Context())
	ws.renderPage(w, "search.html", map[string]any{
		"Projects": projects,
		"Active":   "search",
	})
}

func (ws *WebServer) handleMemories(w http.ResponseWriter, r *http.Request) {
	projects, _ := ws.store.ListProjects(r.Context())

	type topicGroup struct {
		Project store.Project
		Topics  []string
	}
	var groups []topicGroup
	for _, p := range projects {
		mems, _ := ws.store.ListMemories(r.Context(), p.ID, "")
		seen := map[string]bool{}
		var topics []string
		for _, m := range mems {
			if !seen[m.Topic] {
				seen[m.Topic] = true
				topics = append(topics, m.Topic)
			}
		}
		groups = append(groups, topicGroup{Project: p, Topics: topics})
	}

	ws.renderPage(w, "memories.html", map[string]any{
		"Groups": groups,
		"Active": "memories",
	})
}

// --- Helpers ---

func (ws *WebServer) renderPage(w http.ResponseWriter, name string, data any) {
	t, err := ws.tmpl.renderPage(name, data)
	if err != nil {
		slog.Error("render template", "name", name, "error", err)
		http.Error(w, "Template error", 500)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("execute template", "name", name, "error", err)
		http.Error(w, "Template error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (ws *WebServer) renderFragment(w http.ResponseWriter, name string, data any) {
	fragTmpl := ws.tmpl.renderFragment(name)
	var buf bytes.Buffer
	if err := fragTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("render fragment", "name", name, "error", err)
		http.Error(w, "Template error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func queryParam(r *http.Request, name, fallback string) string {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	return v
}

func queryInt(r *http.Request, name string, fallback int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
