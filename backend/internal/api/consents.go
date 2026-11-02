package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type consentRecord struct {
	ID            string     `json:"id"`
	Purpose       string     `json:"purpose"`
	Granted       bool       `json:"granted"`
	PolicyVersion string     `json:"policy_version"`
	RecordedAt    time.Time  `json:"recorded_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type consentInput struct {
	Purpose       string `json:"purpose"`
	PolicyVersion string `json:"policy_version"`
}

func (a *API) createConsent(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	var in consentInput
	if decodeJSON(r, 8<<10, &in) != nil || !validConsentPurpose(in.Purpose) || strings.TrimSpace(in.PolicyVersion) == "" || len(in.PolicyVersion) > 64 {
		writeError(w, 400, "INVALID_CONSENT", "A supported purpose and policy version are required.", nil)
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "CONSENT_WRITE_FAILED", "Consent could not be recorded.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var record consentRecord
	err = tx.QueryRow(r.Context(), `INSERT INTO consents(profile_id,purpose,granted,policy_version,revoked_at) VALUES($1,$2,true,$3,NULL) ON CONFLICT(profile_id,purpose) DO UPDATE SET granted=true,policy_version=EXCLUDED.policy_version,recorded_at=now(),revoked_at=NULL RETURNING id,purpose,granted,policy_version,recorded_at,expires_at,revoked_at`, profileID, in.Purpose, strings.TrimSpace(in.PolicyVersion)).Scan(&record.ID, &record.Purpose, &record.Granted, &record.PolicyVersion, &record.RecordedAt, &record.ExpiresAt, &record.RevokedAt)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'consent.granted','consent',$3,jsonb_build_object('purpose',$4::text,'policy_version',$5::text))`, owner(r), profileID, record.ID, record.Purpose, record.PolicyVersion)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "CONSENT_WRITE_FAILED", "Consent could not be recorded.", nil)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (a *API) getConsent(w http.ResponseWriter, r *http.Request) {
	var record consentRecord
	err := a.db.Pool.QueryRow(r.Context(), `SELECT c.id,c.purpose,c.granted,c.policy_version,c.recorded_at,c.expires_at,c.revoked_at FROM consents c JOIN profiles p ON p.id=c.profile_id WHERE c.id=$1 AND p.owner_id=$2`, chi.URLParam(r, "consentID"), owner(r)).Scan(&record.ID, &record.Purpose, &record.Granted, &record.PolicyVersion, &record.RecordedAt, &record.ExpiresAt, &record.RevokedAt)
	if err != nil {
		writeError(w, 404, "CONSENT_NOT_FOUND", "The requested consent record could not be found.", nil)
		return
	}
	writeJSON(w, 200, record)
}

func (a *API) withdrawConsent(w http.ResponseWriter, r *http.Request) {
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "CONSENT_WITHDRAWAL_FAILED", "Consent could not be withdrawn.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var profileID string
	err = tx.QueryRow(r.Context(), `UPDATE consents c SET granted=false,revoked_at=COALESCE(revoked_at,now()),recorded_at=now() FROM profiles p WHERE c.profile_id=p.id AND p.owner_id=$2 AND c.id=$1 RETURNING c.profile_id`, chi.URLParam(r, "consentID"), owner(r)).Scan(&profileID)
	if err != nil {
		writeError(w, 404, "CONSENT_NOT_FOUND", "The requested consent record could not be found.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'consent.withdrawn','consent',$3)`, owner(r), profileID, chi.URLParam(r, "consentID")); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "CONSENT_WITHDRAWAL_FAILED", "Consent could not be withdrawn.", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"granted": false})
}

func validConsentPurpose(purpose string) bool {
	switch purpose {
	case "health_data", "ai_processing", "research", "medical_record_storage", "document_processing", "health_data_integration", "camera_observation_storage", "research_data_access", "data_sharing", "notifications":
		return true
	}
	return false
}
