package web

import (
	"embed"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

// pageTemplates holds a separate parsed template per page.
// Each page gets: layout + all fragments + its own page template.
type pageTemplates struct {
	pages map[string]*template.Template
}

func loadTemplates() (*pageTemplates, error) {
	funcMap := template.FuncMap{
		"comma":      commaFormat,
		"cost":       costFormat,
		"truncate":   truncate,
		"timeAgo":    timeAgo,
		"scoreColor": scoreColor,
		"scorePct":   scorePct,
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"add":        func(a, b int) int { return a + b },
		"mul":        func(a, b int) int { return a * b },
		"list":       func(items ...string) []string { return items },
		"div":        func(a, b int) int { if b == 0 { return 0 }; return a / b },
	}

	// Parse layout + all fragment templates into a base
	base, err := template.New("base").Funcs(funcMap).ParseFS(templateFS,
		"templates/layout.html",
		"templates/_*.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse base templates: %w", err)
	}

	// For each page, clone the base and parse the page template on top
	pages := map[string]*template.Template{}
	pageFiles := []string{
		"templates/dashboard.html",
		"templates/search.html",
		"templates/history.html",
		"templates/memories.html",
	}
	for _, pf := range pageFiles {
		clone, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone for %s: %w", pf, err)
		}
		t, err := clone.ParseFS(templateFS, pf)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", pf, err)
		}
		// Extract just the filename: "templates/dashboard.html" -> "dashboard.html"
		name := pf[len("templates/"):]
		pages[name] = t
	}

	// Also store fragments for direct rendering
	pages["_fragments"] = base

	return &pageTemplates{pages: pages}, nil
}

func (pt *pageTemplates) renderPage(name string, data any) (*template.Template, error) {
	t, ok := pt.pages[name]
	if !ok {
		return nil, fmt.Errorf("page template %q not found", name)
	}
	return t, nil
}

func (pt *pageTemplates) renderFragment(name string) *template.Template {
	return pt.pages["_fragments"]
}

func commaFormat(n int) string {
	if n < 0 {
		return "-" + commaFormat(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func costFormat(tokens int, pricePerMTok float64) string {
	cost := float64(tokens) / 1_000_000.0 * pricePerMTok
	return fmt.Sprintf("$%.2f", cost)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func scoreColor(score float64) string {
	if score >= 0.7 {
		return "text-emerald-400"
	}
	if score >= 0.4 {
		return "text-yellow-400"
	}
	return "text-zinc-500"
}

func scorePct(score float64) int {
	return int(math.Round(score * 100))
}
