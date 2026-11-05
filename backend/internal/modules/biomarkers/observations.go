package biomarkers

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type observationInput struct {
	BiomarkerCode  string   `json:"biomarker_code"`
	DisplayName    string   `json:"display_name"`
	ValueNumeric   *float64 `json:"value_numeric"`
	Unit           string   `json:"unit"`
	ReferenceRange string   `json:"reference_range"`
	ObservedAt     string   `json:"observed_at"`
	SourceDocument *string  `json:"source_document_id"`
}

type observationRecord struct {
	ID                 string     `json:"id"`
	BiomarkerCode      *string    `json:"biomarker_code,omitempty"`
	DisplayName        string     `json:"display_name"`
	ValueNumeric       float64    `json:"value_numeric"`
	Unit               string     `json:"unit"`
	ReferenceRange     string     `json:"reference_range,omitempty"`
	ObservedAt         *time.Time `json:"observed_at,omitempty"`
	SourceType         string     `json:"source_type"`
	SourceDocumentID   *string    `json:"source_document_id,omitempty"`
	VerificationStatus string     `json:"verification_status"`
	Revision           int        `json:"revision"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (m *Module) createObservation(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	if !m.HasConsent(r.Context(), profileID, "medical_record_storage") && !m.HasConsent(r.Context(), profileID, "health_data") {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Health data consent is required.", nil)
		return
	}
	var in observationInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil || in.ValueNumeric == nil || math.IsNaN(*in.ValueNumeric) || math.IsInf(*in.ValueNumeric, 0) || math.Abs(*in.ValueNumeric) > 1e12 || len(in.DisplayName) > 120 || strings.TrimSpace(in.Unit) == "" || len(in.Unit) > 40 {
		httpx.WriteError(w, 400, "INVALID_OBSERVATION", "Name, numeric value, and unit are required.", nil)
		return
	}
	var code *string
	name := strings.TrimSpace(in.DisplayName)
	if normalized := strings.ToUpper(strings.TrimSpace(in.BiomarkerCode)); normalized != "" {
		var catalogName string
		var units []string
		if err := m.DB.Pool.QueryRow(r.Context(), `SELECT display_name,supported_units FROM biomarker_catalog WHERE code=$1`, normalized).Scan(&catalogName, &units); err != nil {
			httpx.WriteError(w, 400, "UNKNOWN_BIOMARKER", "The biomarker code is not in the catalog.", map[string]string{"biomarker_code": "unknown"})
			return
		}
		if len(units) > 0 && !httpx.Contains(units, strings.TrimSpace(in.Unit)) {
			httpx.WriteError(w, 400, "INCOMPATIBLE_UNIT", "The unit is not supported for this biomarker.", map[string]string{"unit": "unsupported"})
			return
		}
		code = &normalized
		if name == "" {
			name = catalogName
		}
	}
	if name == "" {
		httpx.WriteError(w, 400, "INVALID_OBSERVATION", "A display name or biomarker code is required.", nil)
		return
	}
	observedAt, err := parseObservationDate(in.ObservedAt)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_OBSERVED_AT", "Use an ISO 8601 timestamp or YYYY-MM-DD date.", map[string]string{"observed_at": "invalid"})
		return
	}
	if in.SourceDocument != nil {
		var exists bool
		if err := m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status NOT IN ('deleting','deleted'))`, *in.SourceDocument, profileID, httpx.Owner(r)).Scan(&exists); err != nil || !exists {
			httpx.WriteError(w, 400, "INVALID_SOURCE_DOCUMENT", "The source document is not available in this profile.", nil)
			return
		}
	}
	var record observationRecord
	err = m.DB.Pool.QueryRow(r.Context(), `INSERT INTO observations(profile_id,document_id,owner_id,biomarker_code,name,value,unit,observed_at,reference_range,source_type,verification_status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'MANUAL','PENDING_REVIEW') RETURNING id,biomarker_code,name,value::float8,unit,COALESCE(reference_range,''),observed_at,source,document_id,verification_status,revision,created_at`, profileID, in.SourceDocument, httpx.Owner(r), code, name, *in.ValueNumeric, strings.TrimSpace(in.Unit), observedAt, strings.TrimSpace(in.ReferenceRange)).Scan(&record.ID, &record.BiomarkerCode, &record.DisplayName, &record.ValueNumeric, &record.Unit, &record.ReferenceRange, &record.ObservedAt, &record.SourceType, &record.SourceDocumentID, &record.VerificationStatus, &record.Revision, &record.CreatedAt)
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_CREATE_FAILED", "The observation could not be saved.", nil)
		return
	}
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'observation.created','observation',$3,jsonb_build_object('verification_status','PENDING_REVIEW'))`, httpx.Owner(r), profileID, record.ID)
	httpx.WriteJSON(w, 201, record)
}

func (m *Module) listBiomarkers(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT code,display_name,synonyms,supported_units FROM biomarker_catalog
		WHERE status IN ('approved','legacy_seed')
		   OR code IN (SELECT biomarker_code FROM observations WHERE profile_id=$1 AND owner_id=$2 AND biomarker_code IS NOT NULL)
		ORDER BY display_name`, chi.URLParam(r, "profileID"), httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "BIOMARKER_CATALOG_UNAVAILABLE", "The biomarker catalog is unavailable.", nil)
		return
	}
	defer rows.Close()
	type item struct {
		Code           string   `json:"code"`
		DisplayName    string   `json:"display_name"`
		Synonyms       []string `json:"synonyms"`
		SupportedUnits []string `json:"supported_units"`
	}
	items := []item{}
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.Code, &v.DisplayName, &v.Synonyms, &v.SupportedUnits); err != nil {
			httpx.WriteError(w, 500, "BIOMARKER_CATALOG_UNAVAILABLE", "The biomarker catalog is unavailable.", nil)
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		httpx.WriteError(w, 500, "BIOMARKER_CATALOG_UNAVAILABLE", "The biomarker catalog is unavailable.", nil)
		return
	}
	httpx.WriteJSON(w, 200, items)
}

func (m *Module) biomarkerHistory(w http.ResponseWriter, r *http.Request) {
	profileID, code := chi.URLParam(r, "profileID"), strings.ToUpper(chi.URLParam(r, "code"))
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id,biomarker_code,name,value::float8,unit,COALESCE(reference_range,''),observed_at,source,document_id,verification_status,revision,created_at FROM observations WHERE profile_id=$1 AND owner_id=$2 AND biomarker_code=$3 ORDER BY observed_at DESC NULLS LAST,created_at DESC`, profileID, httpx.Owner(r), code)
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATIONS_UNAVAILABLE", "Observation history is unavailable.", nil)
		return
	}
	defer rows.Close()
	items := []observationRecord{}
	for rows.Next() {
		var o observationRecord
		if err := rows.Scan(&o.ID, &o.BiomarkerCode, &o.DisplayName, &o.ValueNumeric, &o.Unit, &o.ReferenceRange, &o.ObservedAt, &o.SourceType, &o.SourceDocumentID, &o.VerificationStatus, &o.Revision, &o.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "OBSERVATIONS_UNAVAILABLE", "Observation history is unavailable.", nil)
			return
		}
		items = append(items, o)
	}
	if rows.Err() != nil {
		httpx.WriteError(w, 500, "OBSERVATIONS_UNAVAILABLE", "Observation history is unavailable.", nil)
		return
	}
	httpx.WriteJSON(w, 200, items)
}

func (m *Module) verifyObservation(w http.ResponseWriter, r *http.Request) {
	exists, consent, err := m.observationConsent(r, chi.URLParam(r, "observationID"))
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_ACCESS_FAILED", "The observation could not be accessed.", nil)
		return
	}
	if !exists {
		httpx.WriteError(w, 404, "OBSERVATION_NOT_FOUND", "The observation was not found.", nil)
		return
	}
	if !consent {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Health data consent is required.", nil)
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_VERIFY_FAILED", "The observation could not be verified.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var profileID string
	err = tx.QueryRow(r.Context(), `UPDATE observations SET verification_status='VERIFIED' WHERE id=$1 AND owner_id=$2 AND verification_status='PENDING_REVIEW' RETURNING profile_id`, chi.URLParam(r, "observationID"), httpx.Owner(r)).Scan(&profileID)
	if err != nil {
		httpx.WriteError(w, 404, "OBSERVATION_NOT_FOUND", "The observation was not found or is already verified.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'observation.verified','observation',$3)`, httpx.Owner(r), profileID, chi.URLParam(r, "observationID")); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_VERIFY_FAILED", "The observation could not be verified.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]string{"verification_status": "VERIFIED"})
}

type correctionInput struct {
	ValueNumeric   *float64 `json:"value_numeric"`
	Unit           *string  `json:"unit"`
	ReferenceRange *string  `json:"reference_range"`
	ObservedAt     *string  `json:"observed_at"`
	ChangeReason   string   `json:"change_reason"`
}

func (m *Module) correctObservation(w http.ResponseWriter, r *http.Request) {
	exists, consent, err := m.observationConsent(r, chi.URLParam(r, "observationID"))
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_ACCESS_FAILED", "The observation could not be accessed.", nil)
		return
	}
	if !exists {
		httpx.WriteError(w, 404, "OBSERVATION_NOT_FOUND", "The observation was not found.", nil)
		return
	}
	if !consent {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Health data consent is required.", nil)
		return
	}
	var in correctionInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_CORRECTION", "The correction request is invalid.", nil)
		return
	}
	if in.ValueNumeric == nil && in.Unit == nil && in.ReferenceRange == nil && in.ObservedAt == nil {
		httpx.WriteError(w, 400, "INVALID_CORRECTION", "Provide at least one field to correct.", nil)
		return
	}
	if in.ValueNumeric != nil && (math.IsNaN(*in.ValueNumeric) || math.IsInf(*in.ValueNumeric, 0) || math.Abs(*in.ValueNumeric) > 1e12) {
		httpx.WriteError(w, 400, "INVALID_CORRECTION", "Numeric value is invalid.", nil)
		return
	}
	if in.Unit != nil && (strings.TrimSpace(*in.Unit) == "" || len(*in.Unit) > 40) {
		httpx.WriteError(w, 400, "INVALID_CORRECTION", "Unit is invalid.", nil)
		return
	}
	var observedAt *time.Time
	if in.ObservedAt != nil {
		observedAt, err = parseObservationDate(*in.ObservedAt)
		if err != nil {
			httpx.WriteError(w, 400, "INVALID_CORRECTION", "Observation date is invalid.", nil)
			return
		}
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_CORRECTION_FAILED", "The correction could not be saved.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var profileID string
	var revision int
	var snapshot []byte
	err = tx.QueryRow(r.Context(), `SELECT profile_id,revision,jsonb_build_object('biomarker_code',biomarker_code,'name',name,'value_numeric',value,'unit',unit,'reference_range',reference_range,'observed_at',observed_at,'verification_status',verification_status) FROM observations WHERE id=$1 AND owner_id=$2 FOR UPDATE`, chi.URLParam(r, "observationID"), httpx.Owner(r)).Scan(&profileID, &revision, &snapshot)
	if err != nil {
		httpx.WriteError(w, 404, "OBSERVATION_NOT_FOUND", "The requested observation could not be found.", nil)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO observation_revisions(observation_id,profile_id,owner_id,revision,snapshot,change_reason) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, chi.URLParam(r, "observationID"), profileID, httpx.Owner(r), revision, string(snapshot), strings.TrimSpace(in.ChangeReason))
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE observations SET value=COALESCE($2,value),unit=COALESCE($3,unit),reference_range=COALESCE($4,reference_range),observed_at=COALESCE($5,observed_at),revision=revision+1,verification_status='PENDING_REVIEW' WHERE id=$1`, chi.URLParam(r, "observationID"), in.ValueNumeric, in.Unit, in.ReferenceRange, observedAt)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'observation.corrected','observation',$3,jsonb_build_object('revision',$4::int,'change_reason',$5::text))`, httpx.Owner(r), profileID, chi.URLParam(r, "observationID"), revision+1, strings.TrimSpace(in.ChangeReason))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "OBSERVATION_CORRECTION_FAILED", "The correction could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"revision": revision + 1, "verification_status": "PENDING_REVIEW"})
}

func (m *Module) observationConsent(r *http.Request, observationID string) (exists, granted bool, err error) {
	err = m.DB.Pool.QueryRow(r.Context(), `SELECT
		EXISTS(SELECT 1 FROM observations WHERE id=$1 AND owner_id=$2),
		EXISTS(SELECT 1 FROM observations o JOIN consents c ON c.profile_id=o.profile_id WHERE o.id=$1 AND o.owner_id=$2 AND c.granted=true AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>now()) AND c.purpose IN ('medical_record_storage','health_data'))`, observationID, httpx.Owner(r)).Scan(&exists, &granted)
	return exists, granted, err
}

func parseObservationDate(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		v := parsed.UTC()
		return &v, nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return &parsed, nil
	}
	return nil, fmt.Errorf("invalid observation date")
}
