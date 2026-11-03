// Package embedding is the single client for the stateless AI service's embedding endpoints.
// Go keeps ownership of storage and access control; this package only turns text into vectors
// and reports which model produced them so stale vectors can be detected.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cortex/backend/internal/platform/httpx"
)

const (
	Dimensions          = 384
	attempts            = 3
	modelStatusTTL      = 30 * time.Second
	CandidatePoolSize   = 20
	RRFK                = 60
	MinRerankCandidates = 2
)

var (
	modelMu        sync.Mutex
	modelName      string
	modelCheckedAt time.Time
	profileLocks   sync.Map
)

// ServiceURL is the base URL of the AI service.
func ServiceURL() string {
	return strings.TrimRight(strings.TrimSpace(httpx.Getenv("AI_SERVICE_URL", "http://localhost:8000")), "/")
}

// RerankEnabled reports whether the optional cross-encoder rerank step is switched on.
func RerankEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CORTEX_RERANK_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}

// ResetModelCache forgets the cached model name; tests use it when they swap the AI service.
func ResetModelCache() {
	modelMu.Lock()
	modelName = ""
	modelMu.Unlock()
}

// CurrentModel returns the model the AI service will embed with.
func CurrentModel(ctx context.Context) (string, error) {
	modelMu.Lock()
	defer modelMu.Unlock()
	if modelName != "" && time.Since(modelCheckedAt) < modelStatusTTL {
		return modelName, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ServiceURL()+"/v1/embedding", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		State string `json:"state"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("embedding status unavailable (%d)", resp.StatusCode)
	}
	if out.State != "ready" || out.Model == "" {
		return "", fmt.Errorf("embedding model not ready (%s)", out.State)
	}
	modelName, modelCheckedAt = out.Model, time.Now()
	return modelName, nil
}

// ProfileLock serialises indexing work for one profile.
func ProfileLock(profileID string) *sync.Mutex {
	l, _ := profileLocks.LoadOrStore(profileID, &sync.Mutex{})
	return l.(*sync.Mutex)
}

// Embed asks the AI service for vectors, retrying transient failures. kind is "query" or "passage".
func Embed(ctx context.Context, texts []string, kind string) (string, [][]float32, error) {
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		model, vectors, retry, err := embedOnce(ctx, texts, kind)
		if err == nil {
			return model, vectors, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return "", nil, lastErr
}

func embedOnce(ctx context.Context, texts []string, kind string) (string, [][]float32, bool, error) {
	body, _ := json.Marshal(map[string]any{"texts": texts, "kind": kind})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ServiceURL()+"/v1/embed", bytes.NewReader(body))
	if err != nil {
		return "", nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", nil, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, resp.StatusCode >= 500, fmt.Errorf("embedding service returned %d", resp.StatusCode)
	}
	var out struct {
		Model   string      `json:"model"`
		Vectors [][]float32 `json:"vectors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return "", nil, true, err
	}
	if len(out.Vectors) != len(texts) || out.Model == "" {
		return "", nil, false, fmt.Errorf("embedding service returned an unexpected shape")
	}
	for _, v := range out.Vectors {
		if len(v) != Dimensions {
			return "", nil, false, fmt.Errorf("embedding dimension %d, want %d", len(v), Dimensions)
		}
	}
	return out.Model, out.Vectors, false, nil
}

// VectorLiteral formats a vector for a pgvector parameter.
func VectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
