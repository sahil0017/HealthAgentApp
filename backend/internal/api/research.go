package api

import (
	"context"
	"fmt"
)

type researchHit struct {
	SourceID   string  `json:"source_id"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

// minResearchSimilarity keeps unrelated literature out of answers; a keyword match always qualifies.
const minResearchSimilarity = 0.5

// searchResearch retrieves human-approved literature only. It holds public abstracts, so it is not scoped to a profile.
func (a *API) searchResearch(ctx context.Context, query string, limit int) ([]researchHit, error) {
	var have bool
	if err := a.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM research_chunks)`).Scan(&have); err != nil || !have {
		return nil, err
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
		return nil, fmt.Errorf("embedding model changed during request")
	}
	rows, err := a.db.Pool.Query(ctx, `
WITH scope AS (
  SELECT c.id, c.source_id, c.content, c.embedding, c.content_tsv FROM research_chunks c WHERE c.embedding_model=$2
), v AS (
  SELECT id, row_number() OVER (ORDER BY embedding <=> $1::vector) AS rnk, (1-(embedding <=> $1::vector)) AS sim
  FROM scope ORDER BY embedding <=> $1::vector LIMIT $4
), l AS (
  SELECT s.id, row_number() OVER (ORDER BY ts_rank_cd(s.content_tsv, q.q) DESC) AS rnk
  FROM scope s, websearch_to_tsquery('english', $3) q(q) WHERE s.content_tsv @@ q.q LIMIT $4
)
SELECT s.source_id::text, rs.identifier, rs.title, rs.url, s.content,
       (COALESCE(1.0/($5+v.rnk),0)+COALESCE(1.0/($5+l.rnk),0))::float8 AS score
FROM scope s JOIN research_sources rs ON rs.id=s.source_id
LEFT JOIN v USING (id) LEFT JOIN l USING (id)
WHERE l.rnk IS NOT NULL OR v.sim >= $6
ORDER BY score DESC LIMIT $7`, vectorLiteral(vectors[0]), model, query, candidatePoolSize, rrfK, minResearchSimilarity, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []researchHit{}
	for rows.Next() {
		var h researchHit
		if err := rows.Scan(&h.SourceID, &h.Identifier, &h.Title, &h.URL, &h.Content, &h.Score); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}
