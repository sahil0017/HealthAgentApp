package vault

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type documentHistoryEvent struct {
	Action  string          `json:"action"`
	At      time.Time       `json:"at"`
	Details json.RawMessage `json:"details"`
}

// documentHistory lists what happened to a document: processing, type suggestions, owner edits, approval and retries.
// Access events (downloads, searches) belong to the profile access history and are left out.
func (m *Module) documentHistory(w http.ResponseWriter, r *http.Request) {
	d, ok := m.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `
		SELECT action, created_at, details FROM audit_events
		WHERE owner_id=$1 AND resource_type='medical_document' AND resource_id=$2
		  AND action IN ('document.upload_completed','document.processed','document.type_suggested','document.metadata_updated','document.approved','document.retry_requested','document.processing_attempt_failed','document.processing_blocked')
		ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`, httpx.Owner(r), d.ID, limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "HISTORY_UNAVAILABLE", "Document history is unavailable.", nil)
		return
	}
	defer rows.Close()
	events := []documentHistoryEvent{}
	for rows.Next() {
		var e documentHistoryEvent
		if err := rows.Scan(&e.Action, &e.At, &e.Details); err != nil {
			httpx.WriteError(w, 500, "HISTORY_UNAVAILABLE", "Document history is unavailable.", nil)
			return
		}
		events = append(events, e)
	}
	if rows.Err() != nil {
		httpx.WriteError(w, 500, "HISTORY_UNAVAILABLE", "Document history is unavailable.", nil)
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	httpx.WriteJSON(w, 200, events)
}
