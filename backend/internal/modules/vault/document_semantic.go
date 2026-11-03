package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"cortex/backend/internal/platform/embedding"
)

var errEmbeddingModelChanged = errors.New("embedding model changed during request")

const (
	pageChunkChars     = 1200
	maxChunksPerPage   = 8
	maxPagesPerIndexed = 40
)

// chunkText splits page text on whitespace into pieces the embedding model can hold. Text past the cap is not indexed.
func chunkText(text string) []string {
	words := strings.Fields(text)
	var chunks []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && cur.Len()+1+len(w) > pageChunkChars {
			chunks = append(chunks, cur.String())
			cur.Reset()
			if len(chunks) == maxChunksPerPage {
				return chunks
			}
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 && len(chunks) < maxChunksPerPage {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// indexDocumentPages embeds pages that are new, changed since last indexing, or embedded with another model.
func (m *Module) indexDocumentPages(ctx context.Context, ownerID, profileID string) error {
	current, err := embedding.CurrentModel(ctx)
	if err != nil {
		return err
	}
	lock := embedding.ProfileLock(profileID)
	lock.Lock()
	defer lock.Unlock()
	rows, err := m.DB.Pool.Query(ctx, `
		SELECT p.document_id::text, p.page_number, p.extracted_text
		FROM document_extraction_pages p JOIN medical_documents d ON d.id=p.document_id
		WHERE d.profile_id=$1 AND d.owner_id=$2 AND d.status NOT IN ('deleting','deleted') AND length(btrim(p.extracted_text))>0
		  AND NOT EXISTS (SELECT 1 FROM document_page_chunks c WHERE c.document_id=p.document_id AND c.page_number=p.page_number
		                  AND c.embedding_model=$3 AND c.page_sha256=encode(sha256(convert_to(p.extracted_text,'UTF8')),'hex'))
		ORDER BY d.created_at, p.page_number LIMIT $4`, profileID, ownerID, current, maxPagesPerIndexed)
	if err != nil {
		return err
	}
	type page struct {
		doc  string
		num  int
		text string
	}
	var todo []page
	for rows.Next() {
		var p page
		if err := rows.Scan(&p.doc, &p.num, &p.text); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, p := range todo {
		chunks := chunkText(p.text)
		if len(chunks) == 0 {
			continue
		}
		model, vectors, err := embedding.Embed(ctx, chunks, "passage")
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(p.text))
		hash := hex.EncodeToString(sum[:])
		tx, err := m.DB.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM document_page_chunks WHERE document_id=$1 AND page_number=$2`, p.doc, p.num); err != nil {
			tx.Rollback(ctx)
			return err
		}
		for i, c := range chunks {
			if _, err = tx.Exec(ctx, `INSERT INTO document_page_chunks(owner_id,profile_id,document_id,page_number,chunk_index,page_sha256,content,embedding,embedding_model) VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector,$9)`, ownerID, profileID, p.doc, p.num, i, hash, c, embedding.VectorLiteral(vectors[i]), model); err != nil {
				tx.Rollback(ctx)
				return err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// semanticDocumentHits fuses vector and keyword ranking over page chunks and returns the best chunk per page.
func (m *Module) semanticDocumentHits(ctx context.Context, ownerID, profileID, query, docType string, limit int) ([]DocumentSearchHit, error) {
	if err := m.indexDocumentPages(ctx, ownerID, profileID); err != nil {
		return nil, err
	}
	model, vectors, err := embedding.Embed(ctx, []string{query}, "query")
	if err != nil {
		return nil, err
	}
	current, err := embedding.CurrentModel(ctx)
	if err != nil {
		return nil, err
	}
	if model != current {
		return nil, errEmbeddingModelChanged
	}
	rows, err := m.DB.Pool.Query(ctx, `
WITH scope AS (
  SELECT c.id, c.document_id, c.page_number, c.content, c.embedding, c.content_tsv
  FROM document_page_chunks c JOIN medical_documents d ON d.id=c.document_id
  WHERE c.profile_id=$1 AND c.owner_id=$2 AND c.embedding_model=$4 AND d.status NOT IN ('deleting','deleted')
    AND ($7 = '' OR d.document_type=$7)
), v AS (
  SELECT id, row_number() OVER (ORDER BY embedding <=> $3::vector) AS rnk FROM scope ORDER BY embedding <=> $3::vector LIMIT $6
), l AS (
  SELECT s.id, row_number() OVER (ORDER BY ts_rank_cd(s.content_tsv, q.q) DESC) AS rnk
  FROM scope s, websearch_to_tsquery('simple', $5) q(q) WHERE s.content_tsv @@ q.q LIMIT $6
), scored AS (
  SELECT s.document_id, s.page_number, s.content, s.content_tsv,
         (COALESCE(1.0/($8+v.rnk),0)+COALESCE(1.0/($8+l.rnk),0))::float8 AS score
  FROM scope s LEFT JOIN v USING (id) LEFT JOIN l USING (id)
  WHERE v.rnk IS NOT NULL OR l.rnk IS NOT NULL
), best AS (
  SELECT DISTINCT ON (document_id, page_number) * FROM scored ORDER BY document_id, page_number, score DESC
)
SELECT d.id::text, d.file_name, d.document_type, d.document_version, d.status, b.page_number,
       ts_headline('simple', b.content, websearch_to_tsquery('simple',$5), 'MaxFragments=1,MaxWords=30,MinWords=10,StartSel=[,StopSel=]'),
       b.score
FROM best b JOIN medical_documents d ON d.id=b.document_id
ORDER BY b.score DESC, d.created_at DESC, b.page_number LIMIT $9`, profileID, ownerID, embedding.VectorLiteral(vectors[0]), model, query, 40, docType, embedding.RRFK, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []DocumentSearchHit{}
	for rows.Next() {
		h := DocumentSearchHit{Match: "semantic"}
		if err := rows.Scan(&h.DocumentID, &h.FileName, &h.DocumentType, &h.DocumentVersion, &h.Status, &h.Page, &h.Snippet, &h.Rank); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}
