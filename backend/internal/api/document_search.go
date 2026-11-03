package api

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type documentSearchHit struct {
	DocumentID      string  `json:"document_id"`
	FileName        string  `json:"file_name"`
	DocumentType    string  `json:"document_type"`
	DocumentVersion int     `json:"document_version"`
	Status          string  `json:"status"`
	Page            int     `json:"page"`
	Snippet         string  `json:"snippet"`
	Rank            float64 `json:"rank"`
	Match           string  `json:"match"`
}

// searchDocuments finds text across every extracted page. Each hit names the document and page, so it can be checked against the source.
func (a *API) searchDocuments(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 || len(query) > 200 {
		writeError(w, 400, "INVALID_SEARCH_QUERY", "Search with 2 to 200 characters.", nil)
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			writeError(w, 400, "INVALID_PAGINATION", "limit must be between 1 and 50.", nil)
			return
		}
		limit = n
	}
	docType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("document_type")))
	if docType != "" && !validMedicalDocumentType(docType) {
		writeError(w, 400, "INVALID_DOCUMENT_TYPE", "The document type is invalid.", nil)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode != "" && mode != "auto" && mode != "keyword" {
		writeError(w, 400, "INVALID_SEARCH_MODE", "mode must be auto or keyword.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	var hits []documentSearchHit
	// Semantic search sends text to the AI service, so it needs AI consent; keyword search always works.
	if mode != "keyword" && a.hasConsent(r.Context(), profileID, "ai_processing") {
		var err error
		if hits, err = a.semanticDocumentHits(r.Context(), owner(r), profileID, query, docType, limit); err != nil {
			log.Printf("semantic document search unavailable, using keyword search: %v", err)
			hits = nil
		}
	}
	if hits == nil {
		var err error
		if hits, err = a.keywordDocumentHits(r.Context(), owner(r), profileID, query, docType, limit); err != nil {
			writeError(w, 500, "SEARCH_UNAVAILABLE", "Document search is unavailable.", nil)
			return
		}
	}
	// The query text is not logged, as it can itself be health information.
	if _, err := a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2::uuid,'document.searched','profile',$2::text,jsonb_build_object('hit_count',$3::int))`, owner(r), profileID, len(hits)); err != nil {
		writeError(w, 500, "AUDIT_WRITE_FAILED", "Document access could not be recorded.", nil)
		return
	}
	writeJSON(w, 200, hits)
}

func (a *API) keywordDocumentHits(ctx context.Context, ownerID, profileID, query, docType string, limit int) ([]documentSearchHit, error) {
	rows, err := a.db.Pool.Query(ctx, `
		WITH q AS (SELECT websearch_to_tsquery('simple', $3) AS tsq)
		SELECT d.id, d.file_name, d.document_type, d.document_version, d.status, p.page_number,
		       ts_headline('simple', p.extracted_text, q.tsq, 'MaxFragments=1,MaxWords=30,MinWords=10,StartSel=[,StopSel=]'),
		       ts_rank(to_tsvector('simple', p.extracted_text), q.tsq)
		FROM document_extraction_pages p
		JOIN medical_documents d ON d.id = p.document_id, q
		WHERE d.profile_id=$1 AND d.owner_id=$2 AND d.status NOT IN ('deleting','deleted')
		  AND ($4 = '' OR d.document_type = $4)
		  AND to_tsvector('simple', p.extracted_text) @@ q.tsq
		ORDER BY 8 DESC, d.created_at DESC, p.page_number
		LIMIT $5`, profileID, ownerID, query, docType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []documentSearchHit{}
	for rows.Next() {
		h := documentSearchHit{Match: "keyword"}
		if err := rows.Scan(&h.DocumentID, &h.FileName, &h.DocumentType, &h.DocumentVersion, &h.Status, &h.Page, &h.Snippet, &h.Rank); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}
