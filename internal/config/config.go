package config

import (
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL  string
	Transport    string // "stdio" or "sse"
	Port         string
	EmbeddingURL string // external embedding API URL (empty = disabled)
	EmbeddingDim int
	LogLevel     string
	LogFormat    string
	MigrateOnStart    bool
	ExitAfterMigrate  bool
	MigrationsDir     string
}

func Load() *Config {
	dim, _ := strconv.Atoi(envOr("EMBEDDING_DIM", "384"))
	return &Config{
		DatabaseURL:  envOr("DATABASE_URL", "postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable"),
		Transport:    envOr("TRANSPORT", "stdio"),
		Port:         envOr("PORT", "8090"),
		EmbeddingURL: os.Getenv("EMBEDDING_URL"),
		EmbeddingDim: dim,
		LogLevel:     envOr("LOG_LEVEL", "info"),
		LogFormat:    envOr("LOG_FORMAT", "text"),
		MigrationsDir: envOr("MIGRATIONS_DIR", "migrations"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
