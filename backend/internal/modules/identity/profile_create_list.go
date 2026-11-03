package identity

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"
)

type profileInput struct {
	DisplayName string `json:"display_name"`
}

func (m *Module) createProfile(w http.ResponseWriter, r *http.Request) {
	var in profileInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil || strings.TrimSpace(in.DisplayName) == "" || len(strings.TrimSpace(in.DisplayName)) > 120 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "display_name_required"})
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "profile_create_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var id, name string
	err = tx.QueryRow(r.Context(), `INSERT INTO profiles(owner_id, display_name) VALUES($1,$2) RETURNING id, display_name`, httpx.Owner(r), strings.TrimSpace(in.DisplayName)).Scan(&id, &name)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile_create_failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'profile.created','profile',$2::uuid::text)`, httpx.Owner(r), id)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "profile_create_failed"})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id, "display_name": name})
}

func (m *Module) listProfiles(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_pagination"})
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id, display_name, created_at FROM profiles WHERE owner_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, httpx.Owner(r), limit, offset)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "profile_list_failed"})
		return
	}
	defer rows.Close()
	type profile struct {
		ID          string    `json:"id"`
		DisplayName string    `json:"display_name"`
		CreatedAt   time.Time `json:"created_at"`
	}
	items := []profile{}
	for rows.Next() {
		var p profile
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.CreatedAt); err != nil {
			httpx.WriteJSON(w, 500, map[string]string{"error": "profile_list_failed"})
			return
		}
		items = append(items, p)
	}
	if rows.Err() != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "profile_list_failed"})
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	httpx.WriteJSON(w, 200, items)
}
