package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"net/http"

	"github.com/Platform-LSS/devmemory/internal/config"
	"github.com/Platform-LSS/devmemory/internal/embedding"
	mcpserver "github.com/Platform-LSS/devmemory/internal/mcp"
	"github.com/Platform-LSS/devmemory/internal/store"
	"github.com/Platform-LSS/devmemory/internal/web"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	migrate := flag.Bool("migrate", false, "Run database migrations on startup")
	exitAfterMigrate := flag.Bool("exit-after-migrate", false, "Exit after running migrations")
	migrationsDir := flag.String("migrations-dir", "", "Path to migrations directory (default: auto-detect)")
	flag.Parse()

	cfg := config.Load()
	cfg.MigrateOnStart = *migrate
	cfg.ExitAfterMigrate = *exitAfterMigrate
	if *migrationsDir != "" {
		cfg.MigrationsDir = *migrationsDir
	}

	// Set up structured logging
	var handler slog.Handler
	opts := &slog.HandlerOptions{}
	switch cfg.LogLevel {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutting down")
		cancel()
	}()

	// Run migrations if requested
	if cfg.MigrateOnStart {
		dir := findMigrationsDir(cfg.MigrationsDir)
		if dir == "" {
			slog.Error("migrations directory not found", "searched", cfg.MigrationsDir)
			os.Exit(1)
		}
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("connect for migration", "error", err)
			os.Exit(1)
		}
		if err := store.RunMigrations(ctx, pool, dir); err != nil {
			slog.Error("migrations failed", "error", err)
			pool.Close()
			os.Exit(1)
		}
		pool.Close()
		if cfg.ExitAfterMigrate {
			slog.Info("migrations complete, exiting")
			return
		}
	}

	// Connect to database
	pgStore, err := store.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pgStore.Close()

	// Create embedding service
	emb := embedding.New(cfg.EmbeddingURL, cfg.EmbeddingDim)
	slog.Info("embedding service", "status", emb.Status())

	// Create MCP server
	srv := mcpserver.New(pgStore, emb)

	// Start transport
	switch cfg.Transport {
	case "web":
		webSrv, err := web.New(pgStore, emb)
		if err != nil {
			slog.Error("web server init failed", "error", err)
			os.Exit(1)
		}
		// Wire event bus to MCP server for real-time updates
		srv.SetEvents(webSrv.Events())

		slog.Info("starting web dashboard", "port", cfg.Port, "url", fmt.Sprintf("http://localhost:%s", cfg.Port))
		httpSrv := &http.Server{Addr: ":" + cfg.Port, Handler: webSrv.Routes()}
		go func() {
			<-ctx.Done()
			httpSrv.Close()
		}()
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("web server error", "error", err)
			os.Exit(1)
		}
	case "sse":
		slog.Info("starting SSE transport", "port", cfg.Port)
		sseServer := server.NewSSEServer(srv.MCPServer(),
			server.WithBaseURL(fmt.Sprintf("http://localhost:%s", cfg.Port)),
		)
		if err := sseServer.Start(":" + cfg.Port); err != nil {
			slog.Error("SSE server error", "error", err)
			os.Exit(1)
		}
	default:
		// stdio transport (default for Claude Code)
		slog.Info("starting stdio transport")
		stdioServer := server.NewStdioServer(srv.MCPServer())
		if err := stdioServer.Listen(ctx, os.Stdin, os.Stdout); err != nil {
			slog.Error("stdio server error", "error", err)
			os.Exit(1)
		}
	}
}

// findMigrationsDir checks common locations for the migrations directory.
func findMigrationsDir(configured string) string {
	candidates := []string{
		configured,
		"migrations",
		"/migrations",
		"./migrations",
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}
