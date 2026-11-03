package governance

import (
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

func (m *Module) getConsents(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id,purpose,granted,policy_version,recorded_at,expires_at,revoked_at FROM consents WHERE profile_id=$1 ORDER BY purpose`, chi.URLParam(r, "profileID"))
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "consent_read_failed"})
		return
	}
	defer rows.Close()
	type consent struct {
		ID            string     `json:"id"`
		Purpose       string     `json:"purpose"`
		Granted       bool       `json:"granted"`
		PolicyVersion string     `json:"policy_version"`
		RecordedAt    time.Time  `json:"recorded_at"`
		ExpiresAt     *time.Time `json:"expires_at,omitempty"`
		RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	}
	items := []consent{}
	for rows.Next() {
		var c consent
		if err := rows.Scan(&c.ID, &c.Purpose, &c.Granted, &c.PolicyVersion, &c.RecordedAt, &c.ExpiresAt, &c.RevokedAt); err != nil {
			httpx.WriteJSON(w, 500, map[string]string{"error": "consent_read_failed"})
			return
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "consent_read_failed"})
		return
	}
	httpx.WriteJSON(w, 200, items)
}

func (m *Module) setConsent(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	purpose := chi.URLParam(r, "purpose")
	if !validConsentPurpose(purpose) {
		httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_consent_purpose"})
		return
	}
	var in struct {
		Granted       *bool   `json:"granted"`
		PolicyVersion *string `json:"policy_version"`
	}
	if httpx.DecodeJSON(r, 4096, &in) != nil || in.Granted == nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "granted_required"})
		return
	}
	var recorded time.Time
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "consent_write_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	policyVersion := "1"
	if in.PolicyVersion != nil && strings.TrimSpace(*in.PolicyVersion) != "" {
		policyVersion = strings.TrimSpace(*in.PolicyVersion)
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO consents(profile_id,purpose,granted,policy_version,revoked_at) VALUES($1,$2,$3,$4,CASE WHEN $3 THEN NULL ELSE now() END) ON CONFLICT(profile_id,purpose) DO UPDATE SET granted=EXCLUDED.granted,policy_version=EXCLUDED.policy_version,revoked_at=EXCLUDED.revoked_at,recorded_at=now() RETURNING recorded_at`, chi.URLParam(r, "profileID"), purpose, *in.Granted, policyVersion).Scan(&recorded)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "consent_write_failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,details) VALUES($1,$2,'consent.changed','consent',jsonb_build_object('purpose',$3::text,'granted',$4::boolean))`, httpx.Owner(r), chi.URLParam(r, "profileID"), purpose, *in.Granted)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "consent_write_failed"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"purpose": purpose, "granted": *in.Granted, "recorded_at": recorded})
}
