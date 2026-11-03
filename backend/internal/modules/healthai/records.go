package healthai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"cortex/backend/internal/platform/embedding"
)

const (
	indexBatchSize  = 32
	indexMaxPerCall = 128
)

type recordHit struct {
	ObservationID string  `json:"observation_id"`
	Content       string  `json:"content"`
	Score         float64 `json:"score"`
	Similarity    float64 `json:"similarity"`
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
func (m *Module) indexProfileRecords(ctx context.Context, ownerID, profileID string) error {
	current, err := embedding.CurrentModel(ctx)
	if err != nil {
		return err
	}
	lock := embedding.ProfileLock(profileID)
	lock.Lock()
	defer lock.Unlock()
	rows, err := m.DB.Pool.Query(ctx, `SELECT o.id::text,o.revision,o.name,o.value::float8,o.unit,o.observed_at,COALESCE(o.reference_range,'') FROM observations o LEFT JOIN record_chunks c ON c.observation_id=o.id WHERE o.profile_id=$1 AND o.owner_id=$2 AND o.verification_status='VERIFIED' AND (c.id IS NULL OR c.observation_revision<>o.revision OR c.embedding_model<>$4) ORDER BY o.created_at LIMIT $3`, profileID, ownerID, indexMaxPerCall, current)
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
		model, vectors, err := embedding.Embed(ctx, texts, "passage")
		if err != nil {
			return err
		}
		for i, p := range batch {
			if _, err := m.DB.Pool.Exec(ctx, `INSERT INTO record_chunks(owner_id,profile_id,observation_id,observation_revision,content,embedding,embedding_model) VALUES($1,$2,$3,$4,$5,$6::vector,$7) ON CONFLICT(observation_id) DO UPDATE SET observation_revision=EXCLUDED.observation_revision,content=EXCLUDED.content,embedding=EXCLUDED.embedding,embedding_model=EXCLUDED.embedding_model,created_at=now()`, ownerID, profileID, p.id, p.revision, p.content, embedding.VectorLiteral(vectors[i]), model); err != nil {
				return err
			}
		}
	}
	return nil
}

// searchRecords fuses pgvector cosine ranking with Postgres full-text ranking (reciprocal rank fusion) and optionally
// reranks with a cross-encoder. Owner, profile and embedding model are always part of the filter.
func (m *Module) searchRecords(ctx context.Context, ownerID, profileID, query string, limit int) ([]recordHit, error) {
	if limit < 1 || limit > 20 {
		limit = 5
	}
	current, err := embedding.CurrentModel(ctx)
	if err != nil {
		return nil, err
	}
	model, vectors, err := embedding.Embed(ctx, []string{query}, "query")
	if err != nil {
		return nil, err
	}
	if model != current {
		return nil, fmt.Errorf("embedding model changed during request (%s != %s)", model, current)
	}
	pool := embedding.CandidatePoolSize
	if limit > pool {
		pool = limit
	}
	rows, err := m.DB.Pool.Query(ctx, `
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
ORDER BY score DESC, similarity DESC LIMIT $6`, profileID, ownerID, embedding.VectorLiteral(vectors[0]), model, query, pool, embedding.RRFK)
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
	if embedding.RerankEnabled() && len(hits) >= embedding.MinRerankCandidates {
		if reranked, err := m.rerankHits(ctx, query, hits); err != nil {
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

func (m *Module) rerankHits(ctx context.Context, query string, hits []recordHit) ([]recordHit, error) {
	passages := make([]string, len(hits))
	for i, h := range hits {
		passages[i] = h.Content
	}
	body, _ := json.Marshal(map[string]any{"query": query, "passages": passages})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embedding.ServiceURL()+"/v1/rerank", bytes.NewReader(body))
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
