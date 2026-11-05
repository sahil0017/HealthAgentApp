package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"uuid"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const maxDocumentBytes = 15 << 20

type document struct {
	ID                   string          `json:"id"`
	ProfileID            string          `json:"-"`
	ObjectKey            string          `json:"-"`
	FileName             string          `json:"file_name"`
	ContentType          string          `json:"content_type"`
	ByteSize             int64           `json:"byte_size"`
	Status               string          `json:"status"`
	DocumentType         string          `json:"document_type"`
	DocumentVersion      int             `json:"document_version"`
	SupersedesDocumentID *string         `json:"supersedes_document_id,omitempty"`
	SHA256               string          `json:"sha256,omitempty"`
	AttemptCount         int             `json:"attempt_count"`
	ErrorCode            *string         `json:"error_code,omitempty"`
	Extraction           json.RawMessage `json:"extraction,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

func (m *Module) uploadDocument(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	if !m.hasDocumentConsents(r.Context(), profileID) {
		httpx.WriteError(w, http.StatusForbidden, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20))
	if err := r.ParseMultipartForm(maxDocumentBytes); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_or_oversized_upload"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "file_required"})
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxDocumentBytes {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "document_size_must_be_between_1_byte_and_15_mib"})
		return
	}
	name := filepath.Base(strings.TrimSpace(header.Filename))
	if name == "." || name == string(filepath.Separator) || name == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_file_name"})
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0]))
	fileData, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil || len(fileData) == 0 || len(fileData) > maxDocumentBytes {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_or_oversized_upload"})
		return
	}
	detectedType := http.DetectContentType(fileData[:min(len(fileData), 512)])
	if !validDocumentType(contentType) || detectedType != contentType {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_DOCUMENT_TYPE", "Upload a valid PDF, JPEG, or PNG file.", map[string]string{"content_type": "does_not_match_file"})
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "upload_initialization_failed"})
		return
	}
	documentID := hex.EncodeToString(token[:])
	key := profileID + "/" + documentID
	if err := m.Objects.Put(r.Context(), key, contentType, int64(len(fileData)), bytes.NewReader(fileData)); err != nil {
		httpx.WriteJSON(w, 502, map[string]string{"error": "private_storage_unavailable"})
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		_ = m.Objects.Remove(r.Context(), key)
		httpx.WriteJSON(w, 500, map[string]string{"error": "document_record_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var d document
	hash := sha256.Sum256(fileData)
	err = tx.QueryRow(r.Context(), `INSERT INTO medical_documents(profile_id, owner_id, object_key, file_name, content_type, byte_size, document_type, sha256, status) VALUES($1,$2,$3,$4,$5,$6,'OTHER',$7,'queued') RETURNING id,file_name,content_type,byte_size,status,created_at,updated_at`, profileID, httpx.Owner(r), key, name, contentType, len(fileData), hex.EncodeToString(hash[:])).Scan(&d.ID, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		_ = m.Objects.Remove(r.Context(), key)
		httpx.WriteJSON(w, 500, map[string]string{"error": "document_record_failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.uploaded','medical_document',$3)`, httpx.Owner(r), profileID, d.ID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('document.process','medical_document',$1::uuid,jsonb_build_object('document_id',$1::uuid::text))`, d.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		_ = m.Objects.Remove(r.Context(), key)
		httpx.WriteJSON(w, 500, map[string]string{"error": "document_record_failed"})
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, d)
}

func (m *Module) listDocuments(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_pagination"})
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id,profile_id,object_key,file_name,content_type,byte_size,status,document_type,COALESCE(sha256,''),document_version,supersedes_document_id,attempt_count,error_code,extraction,created_at,updated_at FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND status NOT IN ('deleting','deleted') ORDER BY created_at DESC,id LIMIT $3 OFFSET $4`, chi.URLParam(r, "profileID"), httpx.Owner(r), limit, offset)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "documents_unavailable"})
		return
	}
	defer rows.Close()
	items := []document{}
	for rows.Next() {
		var d document
		var extraction []byte
		if err := rows.Scan(&d.ID, &d.ProfileID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.DocumentType, &d.SHA256, &d.DocumentVersion, &d.SupersedesDocumentID, &d.AttemptCount, &d.ErrorCode, &extraction, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteJSON(w, 500, map[string]string{"error": "documents_unavailable"})
			return
		}
		d.Extraction = extraction
		items = append(items, d)
	}
	if rows.Err() != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "documents_unavailable"})
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	httpx.WriteJSON(w, 200, items)
}

func (m *Module) getDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := m.ownedDocument(r)
	if !ok {
		httpx.WriteJSON(w, 404, map[string]string{"error": "document_not_found"})
		return
	}
	httpx.WriteJSON(w, 200, d)
}

func (m *Module) getDocumentByID(w http.ResponseWriter, r *http.Request) {
	d, ok := m.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok || d.Status == "deleted" || d.Status == "deleting" {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	httpx.WriteJSON(w, 200, d)
}

func (m *Module) downloadDocument(w http.ResponseWriter, r *http.Request) {
	var key, filename, contentType string
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT object_key,file_name,content_type FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, chi.URLParam(r, "documentID"), chi.URLParam(r, "profileID"), httpx.Owner(r)).Scan(&key, &filename, &contentType)
	if err != nil {
		httpx.WriteJSON(w, 404, map[string]string{"error": "document_not_found"})
		return
	}
	object, err := m.Objects.Get(r.Context(), key)
	if err != nil {
		httpx.WriteJSON(w, 502, map[string]string{"error": "private_storage_unavailable"})
		return
	}
	defer object.Close()
	if _, err := m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.file_accessed','medical_document',$3)`, httpx.Owner(r), chi.URLParam(r, "profileID"), chi.URLParam(r, "documentID")); err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "audit_write_failed"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(object, maxDocumentBytes+1))
}

func (m *Module) ownedDocument(r *http.Request) (document, bool) {
	var d document
	var extraction []byte
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT id,profile_id,object_key,file_name,content_type,byte_size,status,document_type,COALESCE(sha256,''),document_version,supersedes_document_id,attempt_count,error_code,extraction,created_at,updated_at FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, chi.URLParam(r, "documentID"), chi.URLParam(r, "profileID"), httpx.Owner(r)).Scan(&d.ID, &d.ProfileID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.DocumentType, &d.SHA256, &d.DocumentVersion, &d.SupersedesDocumentID, &d.AttemptCount, &d.ErrorCode, &extraction, &d.CreatedAt, &d.UpdatedAt)
	d.Extraction = extraction
	return d, err == nil
}

type reviewedBiomarker struct {
	CandidateID    *string `json:"candidate_id,omitempty"`
	BiomarkerCode  *string `json:"biomarker_code,omitempty"`
	Page           *int    `json:"page,omitempty"`
	Evidence       *string `json:"evidence,omitempty"`
	Name           string  `json:"name"`
	Value          float64 `json:"value"`
	Unit           string  `json:"unit"`
	ObservedAt     *string `json:"observed_at,omitempty"`
	ReferenceRange string  `json:"reference_range,omitempty"`
}

func (m *Module) retryDocument(w http.ResponseWriter, r *http.Request) {
	profileID, documentID := chi.URLParam(r, "profileID"), chi.URLParam(r, "documentID")
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	if !m.hasDocumentConsents(r.Context(), profileID) {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "document_retry_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), `UPDATE medical_documents SET status='queued',attempt_count=0,retry_after=NULL,error_code=NULL,updated_at=now() WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status='failed'`, documentID, profileID, httpx.Owner(r))
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "document_retry_failed"})
		return
	}
	if result.RowsAffected() != 1 {
		httpx.WriteJSON(w, 409, map[string]string{"error": "document_not_retryable"})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.retry_requested','medical_document',$3)`, httpx.Owner(r), profileID, documentID); err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('document.process','medical_document',$1::uuid,jsonb_build_object('document_id',$1::uuid::text))`, documentID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "document_retry_failed"})
		return
	}
	httpx.WriteJSON(w, 202, map[string]string{"document_id": documentID, "status": "queued"})
}

func (m *Module) approveDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := m.ownedDocument(r)
	if !ok {
		httpx.WriteJSON(w, 404, map[string]string{"error": "document_not_found"})
		return
	}
	if !m.HasConsent(r.Context(), chi.URLParam(r, "profileID"), "medical_record_storage") && !m.HasConsent(r.Context(), chi.URLParam(r, "profileID"), "health_data") {
		httpx.WriteJSON(w, 403, map[string]string{"error": "medical_record_storage_consent_required"})
		return
	}
	if d.Status != "review_required" && d.Status != "manual_review_required" && d.Status != "approved" {
		httpx.WriteJSON(w, 409, map[string]string{"error": "document_not_ready_for_review", "status": d.Status})
		return
	}
	var in struct {
		Biomarkers []reviewedBiomarker `json:"biomarkers"`
	}
	if httpx.DecodeJSON(r, 64<<10, &in) != nil || len(in.Biomarkers) > 100 {
		httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_biomarkers"})
		return
	}
	for _, b := range in.Biomarkers {
		if b.CandidateID != nil {
			if _, err := uuid.Parse(*b.CandidateID); err != nil {
				httpx.WriteError(w, 400, "INVALID_CANDIDATE", "The extraction candidate ID is invalid.", nil)
				return
			}
		}
		if strings.TrimSpace(b.Name) == "" || len(b.Name) > 100 || strings.TrimSpace(b.Unit) == "" || len(b.Unit) > 40 || b.Value != b.Value || b.Value > 1e12 || b.Value < -1e12 {
			httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_biomarker_fields"})
			return
		}
		if b.BiomarkerCode != nil {
			var units []string
			if err := m.DB.Pool.QueryRow(r.Context(), `SELECT supported_units FROM biomarker_catalog WHERE code=$1`, strings.ToUpper(strings.TrimSpace(*b.BiomarkerCode))).Scan(&units); err != nil || (len(units) > 0 && !httpx.Contains(units, strings.TrimSpace(b.Unit))) {
				httpx.WriteError(w, 400, "INCOMPATIBLE_UNIT", "The unit is not supported for this biomarker.", map[string]string{"unit": "unsupported"})
				return
			}
		}
	}
	candidateIDs := make([]string, 0, len(in.Biomarkers))
	for _, b := range in.Biomarkers {
		if b.CandidateID != nil {
			candidateIDs = append(candidateIDs, *b.CandidateID)
		}
	}
	ctx := r.Context()
	tx, err := m.DB.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM observations WHERE document_id=$1 AND profile_id=$2 AND owner_id=$3`, d.ID, chi.URLParam(r, "profileID"), httpx.Owner(r)); err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	for i := range in.Biomarkers {
		b := &in.Biomarkers[i]
		if b.BiomarkerCode == nil || strings.TrimSpace(*b.BiomarkerCode) == "" {
			b.BiomarkerCode = m.ensureCatalog(ctx, tx, b.Name, b.Unit)
		}
	}
	for _, b := range in.Biomarkers {
		_, err = tx.Exec(ctx, `INSERT INTO observations(profile_id,document_id,owner_id,biomarker_code,name,value,unit,observed_at,reference_range,source_type,verification_status) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,$9,'LAB_DOCUMENT','VERIFIED')`, chi.URLParam(r, "profileID"), d.ID, httpx.Owner(r), b.BiomarkerCode, strings.TrimSpace(b.Name), b.Value, strings.TrimSpace(b.Unit), stringValue(b.ObservedAt), strings.TrimSpace(b.ReferenceRange))
		if err != nil {
			httpx.WriteJSON(w, 400, map[string]string{"error": "invalid_observation_date_or_value"})
			return
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE document_extraction_candidates SET review_status=CASE WHEN id=ANY($2::uuid[]) THEN 'accepted' ELSE 'rejected' END WHERE document_id=$1`, d.ID, candidateIDs); err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	_, err = tx.Exec(ctx, `UPDATE medical_documents SET status='approved', updated_at=now() WHERE id=$1`, d.ID)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'document.approved','medical_document',$3,jsonb_build_object('biomarker_count',$4::int))`, httpx.Owner(r), chi.URLParam(r, "profileID"), d.ID, len(in.Biomarkers))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	for _, b := range in.Biomarkers {
		if b.BiomarkerCode != nil && strings.TrimSpace(*b.BiomarkerCode) != "" {
			// Only the analyte code and unit leave the request; values and identities never enter the research queue.
			_ = m.Knowledge.Enqueue(ctx, "unit", strings.ToUpper(strings.TrimSpace(*b.BiomarkerCode))+"|"+strings.TrimSpace(b.Unit))
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{"document_id": d.ID, "status": "approved", "observations_saved": len(in.Biomarkers)})
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ensureCatalog gives a reviewed result with no catalog match its own entry, so the same test from another lab
// joins the same trend. User-reported entries are not used to auto-match future extractions. It returns nil when
// an existing entry with that code uses a different unit, leaving the result unlinked as before.
func (m *Module) ensureCatalog(ctx context.Context, tx pgx.Tx, name, unit string) *string {
	name, unit = strings.TrimSpace(name), strings.TrimSpace(unit)
	code := biomarker.CodeFor(name)
	if code == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO biomarker_catalog(code,display_name,synonyms,supported_units,status,source) VALUES($1,$2,ARRAY[$2]::text[],ARRAY[$3]::text[],'user_reported','report') ON CONFLICT(code) DO NOTHING`, code, name, unit); err != nil {
		return nil
	}
	var units []string
	if err := tx.QueryRow(ctx, `SELECT supported_units FROM biomarker_catalog WHERE code=$1`, code).Scan(&units); err != nil || (len(units) > 0 && !httpx.Contains(units, unit)) {
		return nil
	}
	return &code
}
