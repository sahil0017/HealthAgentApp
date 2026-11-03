package governance

import (
	"encoding/json"
	"net/http"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type AccessEvent struct {
	ID           int64           `json:"id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   *string         `json:"resource_id"`
	Details      json.RawMessage `json:"details"`
	CreatedAt    time.Time       `json:"created_at"`
}

// accessHistory lists who did what to a profile's data, newest first.
func (m *Module) accessHistory(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id,action,resource_type,resource_id,details,created_at FROM audit_events WHERE profile_id=$1 AND owner_id=$2 ORDER BY id DESC LIMIT $3 OFFSET $4`, chi.URLParam(r, "profileID"), httpx.Owner(r), limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "ACCESS_HISTORY_FAILED", "Access history could not be loaded.", nil)
		return
	}
	defer rows.Close()
	events := []AccessEvent{}
	for rows.Next() {
		var e AccessEvent
		if err := rows.Scan(&e.ID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "ACCESS_HISTORY_FAILED", "Access history could not be loaded.", nil)
			return
		}
		events = append(events, e)
	}
	httpx.WriteJSON(w, 200, events)
}

// exportProfile returns everything held about a profile as one JSON document.
// Stored file bytes are not inlined and internal object keys are omitted.
func (m *Module) exportProfile(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID, ownerID := chi.URLParam(r, "profileID"), httpx.Owner(r)
	queries := []struct{ key, sql string }{
		{"profile", `SELECT to_jsonb(p) FROM profiles p WHERE id=$1 AND owner_id=$2`},
		{"consents", `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.recorded_at),'[]') FROM consents c WHERE profile_id=$1 AND $2::text IS NOT NULL`},
		{"documents", `SELECT COALESCE(jsonb_agg(to_jsonb(d)-'object_key' ORDER BY d.created_at),'[]') FROM medical_documents d WHERE profile_id=$1 AND owner_id=$2`},
		{"observations", `SELECT COALESCE(jsonb_agg(to_jsonb(o) ORDER BY o.created_at),'[]') FROM observations o WHERE profile_id=$1 AND owner_id=$2`},
		{"observation_revisions", `SELECT COALESCE(jsonb_agg(to_jsonb(v) ORDER BY v.changed_at),'[]') FROM observation_revisions v WHERE profile_id=$1 AND owner_id=$2`},
		{"history_entries", `SELECT COALESCE(jsonb_agg(to_jsonb(h) ORDER BY h.created_at),'[]') FROM history_entries h WHERE profile_id=$1 AND owner_id=$2`},
		{"goals", `SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY g.created_at),'[]') FROM goals g WHERE profile_id=$1 AND owner_id=$2`},
		{"follow_ups", `SELECT COALESCE(jsonb_agg(to_jsonb(f) ORDER BY f.created_at),'[]') FROM follow_ups f WHERE profile_id=$1 AND owner_id=$2`},
		{"health_questions", `SELECT COALESCE(jsonb_agg(to_jsonb(q) ORDER BY q.created_at),'[]') FROM health_questions q WHERE profile_id=$1 AND owner_id=$2`},
		{"audit_events", `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.id),'[]') FROM audit_events e WHERE profile_id=$1 AND owner_id=$2`},
	}
	out := map[string]json.RawMessage{}
	for _, q := range queries {
		var raw []byte
		if err := m.DB.Pool.QueryRow(r.Context(), q.sql, profileID, ownerID).Scan(&raw); err != nil {
			httpx.WriteError(w, 500, "EXPORT_FAILED", "The export could not be created.", nil)
			return
		}
		out[q.key] = raw
	}
	exportedAt, _ := json.Marshal(time.Now().UTC())
	out["exported_at"] = exportedAt
	if _, err := m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'profile.exported','profile',$2::uuid::text)`, ownerID, profileID); err != nil {
		httpx.WriteError(w, 500, "EXPORT_FAILED", "The export could not be created.", nil)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="cortex-export-`+profileID+`.json"`)
	httpx.WriteJSON(w, 200, out)
}

// deleteProfile permanently removes a profile, its stored files and everything
// derived from them (observations, revisions, embeddings, extraction runs).
// Stored files go first, so a storage failure leaves the data intact and the
// request can be repeated. Audit events are kept as metadata-only evidence.
func (m *Module) deleteProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.DecodeJSON(r, 1<<10, &in); err != nil || in.Confirm != "DELETE" {
		httpx.WriteError(w, 400, "CONFIRMATION_REQUIRED", `Send {"confirm":"DELETE"} to permanently delete this profile.`, nil)
		return
	}
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID, ownerID := chi.URLParam(r, "profileID"), httpx.Owner(r)
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT object_key FROM medical_documents WHERE profile_id=$1 AND owner_id=$2`, profileID, ownerID)
	if err != nil {
		httpx.WriteError(w, 500, "PROFILE_DELETE_FAILED", "The profile could not be deleted.", nil)
		return
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			httpx.WriteError(w, 500, "PROFILE_DELETE_FAILED", "The profile could not be deleted.", nil)
			return
		}
		keys = append(keys, key)
	}
	rows.Close()
	if len(keys) > 0 && m.Remover == nil {
		httpx.WriteError(w, 503, "STORAGE_UNAVAILABLE", "File storage is unavailable; nothing was deleted.", nil)
		return
	}
	for _, key := range keys {
		if err := m.Remover.Remove(r.Context(), key); err != nil {
			httpx.WriteError(w, 502, "PROFILE_DELETE_PENDING", "Some files could not be removed; nothing else was deleted. Try again.", nil)
			return
		}
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "PROFILE_DELETE_FAILED", "The profile could not be deleted.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `DELETE FROM outbox_events WHERE aggregate_type='medical_document' AND aggregate_id IN (SELECT id FROM medical_documents WHERE profile_id=$1 AND owner_id=$2)`, profileID, ownerID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM profiles WHERE id=$1 AND owner_id=$2`, profileID, ownerID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'profile.deleted','profile',$2::uuid::text,jsonb_build_object('files_removed',$3::int))`, ownerID, profileID, len(keys))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "PROFILE_DELETE_FAILED", "The profile could not be deleted.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
