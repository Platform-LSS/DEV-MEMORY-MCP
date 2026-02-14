package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/Platform-LSS/devmemory/internal/embedding"
	"github.com/Platform-LSS/devmemory/internal/store"
)

func main() {
	projectID := flag.String("project", "plss-fhir", "Project ID")
	num := flag.Int("num", 0, "Session number")
	title := flag.String("title", "", "Session title")
	summary := flag.String("summary", "", "Session summary")
	file := flag.String("file", "", "Content file path")
	flag.Parse()

	if *num == 0 || *title == "" {
		log.Fatal("--num and --title required")
	}

	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://devmemory:devmemory@localhost:5434/devmemory?sslmode=disable"
	}
	s, err := store.NewPostgresStore(ctx, dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	embURL := os.Getenv("EMBEDDING_URL")
	if embURL == "" {
		embURL = "http://localhost:8091/embed"
	}
	emb := embedding.New(embURL, 384)

	content := ""
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			log.Fatal(err)
		}
		content = string(data)
	}

	embText := *summary
	if embText == "" {
		embText = *title
	}
	vec := emb.Embed(ctx, embText)

	err = s.CreateSession(ctx, &store.Session{
		ProjectID:  *projectID,
		SessionNum: *num,
		Title:      *title,
		Summary:    *summary,
		Content:    content,
	}, vec)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Session %d saved: %s", *num, *title)
}
