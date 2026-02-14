package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Platform-LSS/devmemory/internal/store"
)

// --- Stats Fragment ---

func (ws *WebServer) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	period := queryParam(r, "period", "24h")
	stats, err := ws.store.GetDashboardStats(r.Context())
	if err != nil {
		http.Error(w, "Error loading stats", 500)
		return
	}
	stats.EmbeddingStatus = ws.embedding.Status()
	ws.renderFragment(w, "_stats.html", map[string]any{
		"Stats":  stats,
		"Period": period,
	})
}

// --- Cost Fragment ---

func (ws *WebServer) handleAPICost(w http.ResponseWriter, r *http.Request) {
	stats, err := ws.store.GetDashboardStats(r.Context())
	if err != nil {
		http.Error(w, "Error loading stats", 500)
		return
	}
	stats.EmbeddingStatus = ws.embedding.Status()
	ws.renderFragment(w, "_cost.html", map[string]any{
		"Stats":  stats,
		"Period": queryParam(r, "period", "24h"),
	})
}

// --- Projects Fragment ---

func (ws *WebServer) handleAPIProjects(w http.ResponseWriter, r *http.Request) {
	stats, err := ws.store.GetDashboardStats(r.Context())
	if err != nil {
		http.Error(w, "Error loading stats", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, p := range stats.Projects {
		ws.tmpl.renderFragment("_project_card.html").ExecuteTemplate(w, "_project_card.html", p)
	}
	if len(stats.Projects) == 0 {
		w.Write([]byte(`<p class="text-zinc-500 col-span-3">No projects registered yet.</p>`))
	}
}

// --- History Fragments ---

func (ws *WebServer) handleAPISessions(w http.ResponseWriter, r *http.Request) {
	projectID := queryParam(r, "project", "")
	if projectID == "" {
		w.Write([]byte(`<p class="text-zinc-500 p-4">Select a project</p>`))
		return
	}
	sessions, err := ws.store.ListSessions(r.Context(), projectID)
	if err != nil {
		slog.Error("list sessions", "error", err)
		http.Error(w, "Error", 500)
		return
	}
	ws.renderFragment(w, "_sessions.html", map[string]any{
		"Sessions":  sessions,
		"ProjectID": projectID,
	})
}

func (ws *WebServer) handleAPISessionDetail(w http.ResponseWriter, r *http.Request) {
	projectID := queryParam(r, "project", "")
	num := queryInt(r, "num", 0)
	if projectID == "" || num == 0 {
		http.Error(w, "Missing params", 400)
		return
	}
	sess, err := ws.store.GetSession(r.Context(), projectID, num)
	if err != nil || sess == nil {
		w.Write([]byte(`<p class="text-zinc-500 p-4">Session not found</p>`))
		return
	}
	ws.renderFragment(w, "_session_detail.html", map[string]any{
		"Session": sess,
	})
}

// --- Search Fragment ---

func (ws *WebServer) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	query := queryParam(r, "q", "")
	if query == "" {
		w.Write([]byte(`<p class="text-zinc-500 p-4">Start typing to search...</p>`))
		return
	}

	emb := ws.embedding.Embed(r.Context(), query)
	results, err := ws.store.SearchAll(r.Context(), query, emb, 10)
	if err != nil {
		slog.Error("search all", "error", err)
		http.Error(w, "Search error", 500)
		return
	}

	searchType := "full-text"
	if emb != nil {
		searchType = "semantic"
	}

	ws.renderFragment(w, "_search_results.html", map[string]any{
		"Query":      query,
		"SearchType": searchType,
		"Memories":   results.Memories,
		"Sessions":   results.Sessions,
		"Files":      results.Files,
	})
}

// --- Memory Fragments ---

func (ws *WebServer) handleAPIMemories(w http.ResponseWriter, r *http.Request) {
	projectID := queryParam(r, "project", "")
	topic := queryParam(r, "topic", "")
	if projectID == "" {
		w.Write([]byte(`<p class="text-zinc-500 p-4">Select a project and topic</p>`))
		return
	}
	memories, err := ws.store.ListMemories(r.Context(), projectID, topic)
	if err != nil {
		slog.Error("list memories", "error", err)
		http.Error(w, "Error", 500)
		return
	}
	ws.renderFragment(w, "_memory_list.html", map[string]any{
		"Memories":  memories,
		"ProjectID": projectID,
		"Topic":     topic,
	})
}

func (ws *WebServer) handleAPIMemoryEdit(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	// We need to find this memory — search by listing all projects
	mem := ws.findMemoryByID(r, id)
	if mem == nil {
		http.Error(w, "Not found", 404)
		return
	}
	ws.renderFragment(w, "_memory_form.html", map[string]any{
		"Memory": mem,
		"IsEdit": true,
	})
}

func (ws *WebServer) handleAPIMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	mem := ws.findMemoryByID(r, id)
	if mem == nil {
		http.Error(w, "Not found", 404)
		return
	}

	// Read form values (supports both form-encoded and JSON)
	var value string
	ct := r.Header.Get("Content-Type")
	if ct == "application/json" {
		body, _ := io.ReadAll(r.Body)
		var data map[string]string
		json.Unmarshal(body, &data)
		value = data["value"]
	} else {
		r.ParseForm()
		value = r.FormValue("value")
	}

	if value == "" {
		value = mem.Value
	}

	emb := ws.embedding.Embed(r.Context(), value)
	err := ws.store.SetMemory(r.Context(), &store.Memory{
		ProjectID: mem.ProjectID,
		Topic:     mem.Topic,
		Key:       mem.Key,
		Value:     value,
	}, emb)
	if err != nil {
		slog.Error("update memory", "error", err)
		http.Error(w, "Error", 500)
		return
	}

	// Return updated memory card
	mem.Value = value
	ws.renderFragment(w, "_memory_card", map[string]any{
		"Memory": mem,
	})
}

func (ws *WebServer) handleAPIMemoryDelete(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	mem := ws.findMemoryByID(r, id)
	if mem == nil {
		http.Error(w, "Not found", 404)
		return
	}

	err := ws.store.DeleteMemory(r.Context(), mem.ProjectID, mem.Topic, mem.Key)
	if err != nil {
		slog.Error("delete memory", "error", err)
		http.Error(w, "Error", 500)
		return
	}

	// Return empty (HTMX will remove the element)
	w.WriteHeader(200)
}

func (ws *WebServer) handleAPIMemoryCreate(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	projectID := r.FormValue("project_id")
	topic := r.FormValue("topic")
	key := r.FormValue("key")
	value := r.FormValue("value")

	if projectID == "" || topic == "" || key == "" || value == "" {
		http.Error(w, "All fields required", 400)
		return
	}

	emb := ws.embedding.Embed(r.Context(), value)
	err := ws.store.SetMemory(r.Context(), &store.Memory{
		ProjectID: projectID,
		Topic:     topic,
		Key:       key,
		Value:     value,
	}, emb)
	if err != nil {
		slog.Error("create memory", "error", err)
		http.Error(w, "Error", 500)
		return
	}

	// Return the new memory list for the topic
	memories, _ := ws.store.ListMemories(r.Context(), projectID, topic)
	ws.renderFragment(w, "_memory_list.html", map[string]any{
		"Memories":  memories,
		"ProjectID": projectID,
		"Topic":     topic,
	})
}

// findMemoryByID searches across all projects for a memory with the given ID.
func (ws *WebServer) findMemoryByID(r *http.Request, id int64) *store.Memory {
	projects, _ := ws.store.ListProjects(r.Context())
	for _, p := range projects {
		mems, _ := ws.store.ListMemories(r.Context(), p.ID, "")
		for _, m := range mems {
			if m.ID == id {
				return &m
			}
		}
	}
	return nil
}
