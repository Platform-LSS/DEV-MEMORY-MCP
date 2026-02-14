package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Service generates vector embeddings from text.
// If URL is empty, embedding is disabled and all methods return nil.
type Service struct {
	url    string
	dim    int
	client *http.Client
}

// New creates an embedding service. If url is empty, the service is disabled.
func New(url string, dim int) *Service {
	return &Service{
		url: url,
		dim: dim,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Enabled returns true if the embedding service is configured.
func (s *Service) Enabled() bool {
	return s.url != ""
}

// Dim returns the configured embedding dimension.
func (s *Service) Dim() int {
	return s.dim
}

// embeddingRequest is the request body for the embedding API.
type embeddingRequest struct {
	Text string `json:"text"`
}

// embeddingResponse is the response body from the embedding API.
type embeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed generates a vector embedding for the given text.
// Returns nil if the service is disabled or an error occurs (non-fatal).
func (s *Service) Embed(ctx context.Context, text string) []float32 {
	if !s.Enabled() || text == "" {
		return nil
	}

	body, err := json.Marshal(embeddingRequest{Text: text})
	if err != nil {
		slog.Warn("embedding marshal error", "error", err)
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("embedding request error", "error", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("embedding call failed", "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn("embedding API error", "status", resp.StatusCode, "body", string(respBody))
		return nil
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Warn("embedding decode error", "error", err)
		return nil
	}

	if len(result.Embedding) != s.dim {
		slog.Warn("embedding dimension mismatch", "expected", s.dim, "got", len(result.Embedding))
		return nil
	}

	return result.Embedding
}

// EmbedBatch generates embeddings for multiple texts.
func (s *Service) EmbedBatch(ctx context.Context, texts []string) [][]float32 {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		results[i] = s.Embed(ctx, t)
	}
	return results
}

// Status returns a human-readable status string.
func (s *Service) Status() string {
	if !s.Enabled() {
		return "disabled (no EMBEDDING_URL configured, using keyword search only)"
	}
	return fmt.Sprintf("enabled (url=%s, dim=%d)", s.url, s.dim)
}
