package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

func (a *API) uploadDocument(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	if !a.hasDocumentConsents(r.Context(), profileID) {
		writeError(w, http.StatusForbidden, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20))
	if err := r.ParseMultipartForm(maxDocumentBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_or_oversized_upload"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_required"})
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxDocumentBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "document_size_must_be_between_1_byte_and_15_mib"})
		return
	}
	name := filepath.Base(strings.TrimSpace(header.Filename))
	if name == "." || name == string(filepath.Separator) || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_file_name"})
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0]))
	fileData, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil || len(fileData) == 0 || len(fileData) > maxDocumentBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_or_oversized_upload"})
		return
	}
	detectedType := http.DetectContentType(fileData[:min(len(fileData), 512)])
	if !validDocumentType(contentType) || detectedType != contentType {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_DOCUMENT_TYPE", "Upload a valid PDF, JPEG, or PNG file.", map[string]string{"content_type": "does_not_match_file"})
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeJSON(w, 500, map[string]string{"error": "upload_initialization_failed"})
		return
	}
	documentID := hex.EncodeToString(token[:])
	key := profileID + "/" + documentID
	if err := a.objects.Put(r.Context(), key, contentType, int64(len(fileData)), bytes.NewReader(fileData)); err != nil {
		writeJSON(w, 502, map[string]string{"error": "private_storage_unavailable"})
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		_ = a.objects.Remove(r.Context(), key)
		writeJSON(w, 500, map[string]string{"error": "document_record_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var d document
	hash := sha256.Sum256(fileData)
	err = tx.QueryRow(r.Context(), `INSERT INTO medical_documents(profile_id, owner_id, object_key, file_name, content_type, byte_size, document_type, sha256, status) VALUES($1,$2,$3,$4,$5,$6,'OTHER',$7,'queued') RETURNING id,file_name,content_type,byte_size,status,created_at,updated_at`, profileID, owner(r), key, name, contentType, len(fileData), hex.EncodeToString(hash[:])).Scan(&d.ID, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		_ = a.objects.Remove(r.Context(), key)
		writeJSON(w, 500, map[string]string{"error": "document_record_failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.uploaded','medical_document',$3)`, owner(r), profileID, d.ID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('document.process','medical_document',$1::uuid,jsonb_build_object('document_id',$1::uuid::text))`, d.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		_ = a.objects.Remove(r.Context(), key)
		writeJSON(w, 500, map[string]string{"error": "document_record_failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, d)
}

func (a *API) hasConsent(ctx context.Context, profileID, purpose string) bool {
	var granted bool
	err := a.db.Pool.QueryRow(ctx, `SELECT granted FROM consents WHERE profile_id=$1 AND purpose=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, profileID, purpose).Scan(&granted)
	return err == nil && granted
}

func (a *API) listDocuments(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_pagination"})
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,profile_id,object_key,file_name,content_type,byte_size,status,document_type,COALESCE(sha256,''),document_version,supersedes_document_id,attempt_count,error_code,extraction,created_at,updated_at FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND status NOT IN ('deleting','deleted') ORDER BY created_at DESC,id LIMIT $3 OFFSET $4`, chi.URLParam(r, "profileID"), owner(r), limit, offset)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "documents_unavailable"})
		return
	}
	defer rows.Close()
	items := []document{}
	for rows.Next() {
		var d document
		var extraction []byte
		if err := rows.Scan(&d.ID, &d.ProfileID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.DocumentType, &d.SHA256, &d.DocumentVersion, &d.SupersedesDocumentID, &d.AttemptCount, &d.ErrorCode, &extraction, &d.CreatedAt, &d.UpdatedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": "documents_unavailable"})
			return
		}
		d.Extraction = extraction
		items = append(items, d)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "documents_unavailable"})
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	writeJSON(w, 200, items)
}

func (a *API) getDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := a.ownedDocument(r)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "document_not_found"})
		return
	}
	writeJSON(w, 200, d)
}

func (a *API) getDocumentByID(w http.ResponseWriter, r *http.Request) {
	d, ok := a.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok || d.Status == "deleted" || d.Status == "deleting" {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	writeJSON(w, 200, d)
}

func (a *API) downloadDocument(w http.ResponseWriter, r *http.Request) {
	var key, filename, contentType string
	err := a.db.Pool.QueryRow(r.Context(), `SELECT object_key,file_name,content_type FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, chi.URLParam(r, "documentID"), chi.URLParam(r, "profileID"), owner(r)).Scan(&key, &filename, &contentType)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "document_not_found"})
		return
	}
	object, err := a.objects.Get(r.Context(), key)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "private_storage_unavailable"})
		return
	}
	defer object.Close()
	if _, err := a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.file_accessed','medical_document',$3)`, owner(r), chi.URLParam(r, "profileID"), chi.URLParam(r, "documentID")); err != nil {
		writeJSON(w, 500, map[string]string{"error": "audit_write_failed"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(object, maxDocumentBytes+1))
}

func (a *API) ownedDocument(r *http.Request) (document, bool) {
	var d document
	var extraction []byte
	err := a.db.Pool.QueryRow(r.Context(), `SELECT id,profile_id,object_key,file_name,content_type,byte_size,status,document_type,COALESCE(sha256,''),document_version,supersedes_document_id,attempt_count,error_code,extraction,created_at,updated_at FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, chi.URLParam(r, "documentID"), chi.URLParam(r, "profileID"), owner(r)).Scan(&d.ID, &d.ProfileID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.DocumentType, &d.SHA256, &d.DocumentVersion, &d.SupersedesDocumentID, &d.AttemptCount, &d.ErrorCode, &extraction, &d.CreatedAt, &d.UpdatedAt)
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

func (a *API) retryDocument(w http.ResponseWriter, r *http.Request) {
	profileID, documentID := chi.URLParam(r, "profileID"), chi.URLParam(r, "documentID")
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	if !a.hasDocumentConsents(r.Context(), profileID) {
		writeError(w, 403, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "document_retry_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), `UPDATE medical_documents SET status='queued',attempt_count=0,retry_after=NULL,error_code=NULL,updated_at=now() WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status='failed'`, documentID, profileID, owner(r))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "document_retry_failed"})
		return
	}
	if result.RowsAffected() != 1 {
		writeJSON(w, 409, map[string]string{"error": "document_not_retryable"})
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.retry_requested','medical_document',$3)`, owner(r), profileID, documentID); err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('document.process','medical_document',$1::uuid,jsonb_build_object('document_id',$1::uuid::text))`, documentID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "document_retry_failed"})
		return
	}
	writeJSON(w, 202, map[string]string{"document_id": documentID, "status": "queued"})
}

func (a *API) approveDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := a.ownedDocument(r)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "document_not_found"})
		return
	}
	if !a.hasConsent(r.Context(), chi.URLParam(r, "profileID"), "medical_record_storage") && !a.hasConsent(r.Context(), chi.URLParam(r, "profileID"), "health_data") {
		writeJSON(w, 403, map[string]string{"error": "medical_record_storage_consent_required"})
		return
	}
	if d.Status != "review_required" && d.Status != "manual_review_required" && d.Status != "approved" {
		writeJSON(w, 409, map[string]string{"error": "document_not_ready_for_review", "status": d.Status})
		return
	}
	var in struct {
		Biomarkers []reviewedBiomarker `json:"biomarkers"`
	}
	if decodeJSON(r, 64<<10, &in) != nil || len(in.Biomarkers) > 100 {
		writeJSON(w, 400, map[string]string{"error": "invalid_biomarkers"})
		return
	}
	for _, b := range in.Biomarkers {
		if b.CandidateID != nil {
			if _, err := uuid.Parse(*b.CandidateID); err != nil {
				writeError(w, 400, "INVALID_CANDIDATE", "The extraction candidate ID is invalid.", nil)
				return
			}
		}
		if strings.TrimSpace(b.Name) == "" || len(b.Name) > 100 || strings.TrimSpace(b.Unit) == "" || len(b.Unit) > 40 || b.Value != b.Value || b.Value > 1e12 || b.Value < -1e12 {
			writeJSON(w, 400, map[string]string{"error": "invalid_biomarker_fields"})
			return
		}
		if b.BiomarkerCode != nil {
			var units []string
			if err := a.db.Pool.QueryRow(r.Context(), `SELECT supported_units FROM biomarker_catalog WHERE code=$1`, strings.ToUpper(strings.TrimSpace(*b.BiomarkerCode))).Scan(&units); err != nil || (len(units) > 0 && !contains(units, strings.TrimSpace(b.Unit))) {
				writeError(w, 400, "INCOMPATIBLE_UNIT", "The unit is not supported for this biomarker.", map[string]string{"unit": "unsupported"})
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
	tx, err := a.db.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM observations WHERE document_id=$1 AND profile_id=$2 AND owner_id=$3`, d.ID, chi.URLParam(r, "profileID"), owner(r)); err != nil {
		writeJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	for _, b := range in.Biomarkers {
		_, err = tx.Exec(ctx, `INSERT INTO observations(profile_id,document_id,owner_id,biomarker_code,name,value,unit,observed_at,reference_range,source_type,verification_status) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,$9,'LAB_DOCUMENT','VERIFIED')`, chi.URLParam(r, "profileID"), d.ID, owner(r), b.BiomarkerCode, strings.TrimSpace(b.Name), b.Value, strings.TrimSpace(b.Unit), stringValue(b.ObservedAt), strings.TrimSpace(b.ReferenceRange))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid_observation_date_or_value"})
			return
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE document_extraction_candidates SET review_status=CASE WHEN id=ANY($2::uuid[]) THEN 'accepted' ELSE 'rejected' END WHERE document_id=$1`, d.ID, candidateIDs); err != nil {
		writeJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	_, err = tx.Exec(ctx, `UPDATE medical_documents SET status='approved', updated_at=now() WHERE id=$1`, d.ID)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'document.approved','medical_document',$3,jsonb_build_object('biomarker_count',$4::int))`, owner(r), chi.URLParam(r, "profileID"), d.ID, len(in.Biomarkers))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "review_save_failed"})
		return
	}
	for _, b := range in.Biomarkers {
		if b.BiomarkerCode != nil && strings.TrimSpace(*b.BiomarkerCode) != "" {
			// Only the analyte code and unit leave the request; values and identities never enter the research queue.
			_ = a.knowledge.Enqueue(ctx, "unit", strings.ToUpper(strings.TrimSpace(*b.BiomarkerCode))+"|"+strings.TrimSpace(b.Unit))
		}
	}
	writeJSON(w, 200, map[string]any{"document_id": d.ID, "status": "approved", "observations_saved": len(in.Biomarkers)})
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *API) listObservations(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_pagination"})
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,name,value::float8,unit,observed_at,COALESCE(reference_range,''),document_id,created_at,biomarker_code,source_type,verification_status,revision FROM observations WHERE profile_id=$1 AND owner_id=$2 ORDER BY name,observed_at DESC NULLS LAST,id LIMIT $3 OFFSET $4`, chi.URLParam(r, "profileID"), owner(r), limit, offset)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "observations_unavailable"})
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
			writeJSON(w, 500, map[string]string{"error": "observations_unavailable"})
			return
		}
		items = append(items, o)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "observations_unavailable"})
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	writeJSON(w, 200, items)
}

type answerRequest struct {
	Question string `json:"question"`
}

func (a *API) askQuestion(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	if !a.hasConsent(r.Context(), profileID, "medical_record_storage") && !a.hasConsent(r.Context(), profileID, "health_data") {
		writeError(w, 403, "CONSENT_REQUIRED", "Health data consent is required.", nil)
		return
	}
	if !a.hasConsent(r.Context(), profileID, "ai_processing") {
		writeError(w, 403, "CONSENT_REQUIRED", "AI processing consent is required.", nil)
		return
	}
	var in answerRequest
	if decodeJSON(r, 16<<10, &in) != nil || strings.TrimSpace(in.Question) == "" {
		writeJSON(w, 400, map[string]string{"error": "question_required"})
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT name,value::float8,unit,observed_at,COALESCE(reference_range,'') FROM observations WHERE profile_id=$1 AND owner_id=$2 AND verification_status='VERIFIED' ORDER BY name,observed_at DESC NULLS LAST LIMIT 500`, profileID, owner(r))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "context_unavailable"})
		return
	}
	type item struct {
		Name           string     `json:"name"`
		Value          float64    `json:"value"`
		Unit           string     `json:"unit"`
		ObservedAt     *time.Time `json:"observed_at,omitempty"`
		ReferenceRange string     `json:"reference_range,omitempty"`
	}
	contextItems := []item{}
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.Name, &v.Value, &v.Unit, &v.ObservedAt, &v.ReferenceRange); err != nil {
			rows.Close()
			writeJSON(w, 500, map[string]string{"error": "context_unavailable"})
			return
		}
		contextItems = append(contextItems, v)
	}
	if rows.Err() != nil {
		rows.Close()
		writeJSON(w, 500, map[string]string{"error": "context_unavailable"})
		return
	}
	rows.Close()
	chunks := []recordHit{}
	if len(contextItems) > 0 {
		if err := a.indexProfileRecords(r.Context(), owner(r), profileID); err != nil {
			log.Printf("record indexing unavailable: %v", err)
		} else if hits, err := a.searchRecords(r.Context(), owner(r), profileID, strings.TrimSpace(in.Question), 5); err != nil {
			log.Printf("record search unavailable: %v", err)
		} else {
			chunks = hits
		}
	}
	research, err := a.searchResearch(r.Context(), strings.TrimSpace(in.Question), 3)
	if err != nil {
		log.Printf("research search unavailable: %v", err)
		research = []researchHit{}
	}
	body, _ := json.Marshal(map[string]any{"question": strings.TrimSpace(in.Question), "observations": contextItems, "record_chunks": chunks, "research_chunks": research})
	service := aiServiceURL()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, service+"/v1/answer", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "ai_service_unavailable"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "ai_service_unavailable"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "ai_service_error"})
		return
	}
	if _, err := a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,details) VALUES($1,$2,'question.asked','agent_run',jsonb_build_object('context_observation_count',$3::int))`, owner(r), profileID, len(contextItems)); err != nil {
		writeJSON(w, 500, map[string]string{"error": "audit_write_failed"})
		return
	}
	var answer json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "ai_service_error"})
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
