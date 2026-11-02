package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	embeddingDimensions = 384
	indexBatchSize      = 32
	indexMaxPerCall     = 128
)

type recordHit struct {
	ObservationID string  `json:"observation_id"`
	Content       string  `json:"content"`
	Score         float64 `json:"score"`
	Similarity    float64 `json:"similarity"`
}

const (
	embeddingAttempts   = 3
	candidatePoolSize   = 20
	modelStatusTTL      = 30 * time.Second
	rrfK                = 60
	minRerankCandidates = 2
)

var (
	modelMu        sync.Mutex
	modelName      string
	modelCheckedAt time.Time
	profileLocks   sync.Map
)

func rerankEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CORTEX_RERANK_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}

// currentEmbeddingModel returns the model the AI service will embed with, so stale vectors can be detected.
func currentEmbeddingModel(ctx context.Context) (string, error) {
	modelMu.Lock()
	defer modelMu.Unlock()
	if modelName != "" && time.Since(modelCheckedAt) < modelStatusTTL {
		return modelName, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aiServiceURL()+"/v1/embedding", nil)
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

func profileLock(profileID string) *sync.Mutex {
	l, _ := profileLocks.LoadOrStore(profileID, &sync.Mutex{})
	return l.(*sync.Mutex)
}

func aiServiceURL() string {
	return strings.TrimRight(strings.TrimSpace(getenv("AI_SERVICE_URL", "http://localhost:8000")), "/")
}

// embed asks the stateless AI service for vectors, retrying transient failures; Go keeps ownership of storage and access control.
func (a *API) embed(ctx context.Context, texts []string, kind string) (string, [][]float32, error) {
	var lastErr error
	for attempt := 0; attempt < embeddingAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		model, vectors, retry, err := a.embedOnce(ctx, texts, kind)
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

func (a *API) embedOnce(ctx context.Context, texts []string, kind string) (string, [][]float32, bool, error) {
	body, _ := json.Marshal(map[string]any{"texts": texts, "kind": kind})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aiServiceURL()+"/v1/embed", bytes.NewReader(body))
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
		if len(v) != embeddingDimensions {
			return "", nil, false, fmt.Errorf("embedding dimension %d, want %d", len(v), embeddingDimensions)
		}
	}
	return out.Model, out.Vectors, false, nil
}

func vectorLiteral(v []float32) string {
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

func observationText(name string, value float64, unit string, observedAt *time.Time, referenceRange string) string {
	text := fmt.Sprintf("%s: %s %s", name, strconv.FormatFloat(value, 'g', -1, 64), unit)
	if observedAt != nil {
		text += " on " + observedAt.Format("2006-01-02")
	}
	if referenceRange != "" {
		text += " (reference " + referenceRange + ")"
	}
	return text
}

// indexProfileRecords embeds verified observations whose chunk is missing, outdated, or made by a different model.
func (a *API) indexProfileRecords(ctx context.Context, ownerID, profileID string) error {
	current, err := currentEmbeddingModel(ctx)
	if err != nil {
		return err
	}
	lock := profileLock(profileID)
	lock.Lock()
	defer lock.Unlock()
	rows, err := a.db.Pool.Query(ctx, `SELECT o.id::text,o.revision,o.name,o.value::float8,o.unit,o.observed_at,COALESCE(o.reference_range,'') FROM observations o LEFT JOIN record_chunks c ON c.observation_id=o.id WHERE o.profile_id=$1 AND o.owner_id=$2 AND o.verification_status='VERIFIED' AND (c.id IS NULL OR c.observation_revision<>o.revision OR c.embedding_model<>$4) ORDER BY o.created_at LIMIT $3`, profileID, ownerID, indexMaxPerCall, current)
	if err != nil {
		return err
	}
	type pending struct {
		id       string
		revision int
		content  string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		var name, unit, rng string
		var value float64
		var observedAt *time.Time
		if err := rows.Scan(&p.id, &p.revision, &name, &value, &unit, &observedAt, &rng); err != nil {
			rows.Close()
			return err
		}
		p.content = observationText(name, value, unit, observedAt, rng)
		todo = append(todo, p)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for start := 0; start < len(todo); start += indexBatchSize {
		end := min(start+indexBatchSize, len(todo))
		batch := todo[start:end]
		texts := make([]string, len(batch))
		for i, p := range batch {
			texts[i] = p.content
		}
		model, vectors, err := a.embed(ctx, texts, "passage")
		if err != nil {
			return err
		}
		for i, p := range batch {
			if _, err := a.db.Pool.Exec(ctx, `INSERT INTO record_chunks(owner_id,profile_id,observation_id,observation_revision,content,embedding,embedding_model) VALUES($1,$2,$3,$4,$5,$6::vector,$7) ON CONFLICT(observation_id) DO UPDATE SET observation_revision=EXCLUDED.observation_revision,content=EXCLUDED.content,embedding=EXCLUDED.embedding,embedding_model=EXCLUDED.embedding_model,created_at=now()`, ownerID, profileID, p.id, p.revision, p.content, vectorLiteral(vectors[i]), model); err != nil {
				return err
			}
		}
	}
	return nil
}

// searchRecords fuses pgvector cosine ranking with Postgres full-text ranking (reciprocal rank fusion) and optionally
// reranks with a cross-encoder. Owner, profile and embedding model are always part of the filter.
func (a *API) searchRecords(ctx context.Context, ownerID, profileID, query string, limit int) ([]recordHit, error) {
	if limit < 1 || limit > 20 {
		limit = 5
	}
	current, err := currentEmbeddingModel(ctx)
	if err != nil {
		return nil, err
	}
	model, vectors, err := a.embed(ctx, []string{query}, "query")
	if err != nil {
		return nil, err
	}
	if model != current {
		return nil, fmt.Errorf("embedding model changed during request (%s != %s)", model, current)
	}
	pool := candidatePoolSize
	if limit > pool {
		pool = limit
	}
	rows, err := a.db.Pool.Query(ctx, `
WITH scope AS (
  SELECT c.observation_id, c.content, c.embedding, c.content_tsv
  FROM record_chunks c JOIN observations o ON o.id=c.observation_id
  WHERE c.profile_id=$1 AND c.owner_id=$2 AND c.embedding_model=$4 AND o.verification_status='VERIFIED'
), v AS (
  SELECT observation_id, row_number() OVER (ORDER BY embedding <=> $3::vector) AS rnk, (1-(embedding <=> $3::vector)) AS sim
  FROM scope ORDER BY embedding <=> $3::vector LIMIT $6
), l AS (
  SELECT s.observation_id, row_number() OVER (ORDER BY ts_rank_cd(s.content_tsv, q.q) DESC) AS rnk
  FROM scope s, websearch_to_tsquery('english', $5) q(q)
  WHERE s.content_tsv @@ q.q LIMIT $6
)
SELECT s.observation_id::text, s.content,
       (COALESCE(1.0/($7+v.rnk),0)+COALESCE(1.0/($7+l.rnk),0))::float8 AS score,
       COALESCE(v.sim,0)::float8 AS similarity
FROM scope s LEFT JOIN v USING (observation_id) LEFT JOIN l USING (observation_id)
WHERE v.rnk IS NOT NULL OR l.rnk IS NOT NULL
ORDER BY score DESC, similarity DESC LIMIT $6`, profileID, ownerID, vectorLiteral(vectors[0]), model, query, pool, rrfK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []recordHit{}
	for rows.Next() {
		var h recordHit
		if err := rows.Scan(&h.ObservationID, &h.Content, &h.Score, &h.Similarity); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if rerankEnabled() && len(hits) >= minRerankCandidates {
		if reranked, err := a.rerankHits(ctx, query, hits); err != nil {
			log.Printf("rerank unavailable, keeping fused order: %v", err)
		} else {
			hits = reranked
		}
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (a *API) rerankHits(ctx context.Context, query string, hits []recordHit) ([]recordHit, error) {
	passages := make([]string, len(hits))
	for i, h := range hits {
		passages[i] = h.Content
	}
	body, _ := json.Marshal(map[string]any{"query": query, "passages": passages})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aiServiceURL()+"/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Scores []float64 `json:"scores"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil || len(out.Scores) != len(hits) {
		return nil, fmt.Errorf("rerank service returned %d", resp.StatusCode)
	}
	for i := range hits {
		hits[i].Score = out.Scores[i]
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits, nil
}
