package biomarkers

import (
	"net/http"
	"strconv"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

func (m *Module) listObservations(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_pagination"})
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id,name,value::float8,unit,observed_at,COALESCE(reference_range,''),document_id,created_at,biomarker_code,source_type,verification_status,revision FROM observations WHERE profile_id=$1 AND owner_id=$2 ORDER BY name,observed_at DESC NULLS LAST,id LIMIT $3 OFFSET $4`, chi.URLParam(r, "profileID"), httpx.Owner(r), limit, offset)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "observations_unavailable"})
		return
	}
	defer rows.Close()
	type observation struct {
		ID                 string     `json:"id"`
		Name               string     `json:"name"`
		Value              float64    `json:"value"`
		Unit               string     `json:"unit"`
		ObservedAt         *time.Time `json:"observed_at,omitempty"`
		ReferenceRange     string     `json:"reference_range,omitempty"`
		DocumentID         *string    `json:"document_id,omitempty"`
		CreatedAt          time.Time  `json:"created_at"`
		BiomarkerCode      *string    `json:"biomarker_code,omitempty"`
		SourceType         string     `json:"source_type"`
		VerificationStatus string     `json:"verification_status"`
		Revision           int        `json:"revision"`
	}
	items := []observation{}
	for rows.Next() {
		var o observation
		if err := rows.Scan(&o.ID, &o.Name, &o.Value, &o.Unit, &o.ObservedAt, &o.ReferenceRange, &o.DocumentID, &o.CreatedAt, &o.BiomarkerCode, &o.SourceType, &o.VerificationStatus, &o.Revision); err != nil {
			httpx.WriteJSON(w, 500, map[string]string{"error": "observations_unavailable"})
			return
		}
		items = append(items, o)
	}
	if rows.Err() != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "observations_unavailable"})
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	httpx.WriteJSON(w, 200, items)
}
