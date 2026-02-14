package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations executes SQL migration files from the given directory.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	// Create migrations tracking table
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Find migration files
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(files)

	for _, f := range files {
		version := filepath.Base(f)

		// Check if already applied
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).
			Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		// Read and execute
		sql, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		slog.Info("applying migration", "version", version)
		_, err = pool.Exec(ctx, string(sql))
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}

		// Record
		_, err = pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		if err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}

	slog.Info("migrations complete")
	return nil
}
