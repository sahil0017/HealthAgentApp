package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const (
	uploadIntentLifetime = 15 * time.Minute
	maxDownloadURLAge    = 5 * time.Minute
)

type uploadIntentInput struct {
	Filename             string  `json:"filename"`
	ContentType          string  `json:"content_type"`
	SizeBytes            int64   `json:"size_bytes"`
	SHA256               string  `json:"sha256"`
	DocumentType         string  `json:"document_type"`
	SupersedesDocumentID *string `json:"supersedes_document_id"`
}

type uploadIntentResponse struct {
	DocumentID           string    `json:"document_id"`
	UploadURL            string    `json:"upload_url"`
	ExpiresAt            time.Time `json:"expires_at"`
	Status               string    `json:"status"`
	DocumentVersion      int       `json:"document_version"`
	SupersedesDocumentID *string   `json:"supersedes_document_id,omitempty"`
}

func (m *Module) createUploadIntent(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	if !m.hasDocumentConsents(r.Context(), profileID) {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	var in uploadIntentInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_UPLOAD_INTENT", "The upload request is invalid.", nil)
		return
	}
	filename := filepath.Base(strings.TrimSpace(in.Filename))
	contentType, _, contentTypeErr := mime.ParseMediaType(in.ContentType)
	if contentTypeErr != nil || filename == "." || filename == ".." || filename == "" || len(filename) > 255 || in.SizeBytes < 1 || in.SizeBytes > maxDocumentBytes || !validDocumentType(contentType) || !validMedicalDocumentType(in.DocumentType) {
		httpx.WriteError(w, 400, "INVALID_UPLOAD_INTENT", "The filename, type, document category, or size is invalid.", nil)
		return
	}
	documentVersion := 1
	if in.SupersedesDocumentID != nil {
		var previousVersion int
		if err := m.DB.Pool.QueryRow(r.Context(), `SELECT document_version FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status NOT IN ('deleting','deleted')`, *in.SupersedesDocumentID, profileID, httpx.Owner(r)).Scan(&previousVersion); err != nil {
			httpx.WriteError(w, 404, "SOURCE_DOCUMENT_NOT_FOUND", "The document being replaced could not be found.", nil)
			return
		}
		documentVersion = previousVersion + 1
	}
	checksum, err := hex.DecodeString(strings.TrimSpace(in.SHA256))
	if err != nil || len(checksum) != sha256.Size {
		httpx.WriteError(w, 400, "INVALID_CHECKSUM", "SHA-256 must be 64 hexadecimal characters.", map[string]string{"sha256": "invalid"})
		return
	}
	sha256Hex := strings.ToLower(strings.TrimSpace(in.SHA256))
	var duplicateID string
	err = m.DB.Pool.QueryRow(r.Context(), `SELECT id FROM medical_documents WHERE profile_id=$1 AND sha256=$2 AND status NOT IN ('deleting','deleted') ORDER BY created_at LIMIT 1`, profileID, sha256Hex).Scan(&duplicateID)
	if err == nil {
		httpx.WriteError(w, http.StatusConflict, "DUPLICATE_DOCUMENT", "This file is already stored for this profile.", map[string]string{"document_id": duplicateID})
		return
	}
	if err != pgx.ErrNoRows {
		httpx.WriteError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		httpx.WriteError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	key := profileID + "/" + hex.EncodeToString(random[:])
	expires := time.Now().UTC().Add(uploadIntentLifetime)
	uploadURL, err := m.Objects.PresignedPut(r.Context(), key, uploadIntentLifetime)
	if err != nil {
		httpx.WriteError(w, 502, "OBJECT_STORAGE_UNAVAILABLE", "Private object storage is unavailable.", nil)
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var documentID string
	err = tx.QueryRow(r.Context(), `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,document_type,sha256,status,upload_expires_at,document_version,supersedes_document_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'initiated',$9,$10,$11) RETURNING id`, profileID, httpx.Owner(r), key, filename, contentType, in.SizeBytes, strings.ToUpper(in.DocumentType), sha256Hex, expires, documentVersion, in.SupersedesDocumentID).Scan(&documentID)
	if err != nil {
		var existingID string
		if lookupErr := m.DB.Pool.QueryRow(r.Context(), `SELECT id FROM medical_documents WHERE profile_id=$1 AND sha256=$2 AND status NOT IN ('deleting','deleted') ORDER BY created_at LIMIT 1`, profileID, sha256Hex).Scan(&existingID); lookupErr == nil {
			httpx.WriteError(w, http.StatusConflict, "DUPLICATE_DOCUMENT", "This file is already stored for this profile.", map[string]string{"document_id": existingID})
			return
		}
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.upload_intent_created','medical_document',$3)`, httpx.Owner(r), profileID, documentID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, uploadIntentResponse{DocumentID: documentID, UploadURL: uploadURL.String(), ExpiresAt: expires, Status: "INITIATED", DocumentVersion: documentVersion, SupersedesDocumentID: in.SupersedesDocumentID})
}

func (m *Module) completeUpload(w http.ResponseWriter, r *http.Request) {
	d, ok := m.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	if !m.hasDocumentConsents(r.Context(), d.ProfileID) {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	if d.Status != "initiated" {
		httpx.WriteJSON(w, 200, d)
		return
	}
	var expectedExpires time.Time
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT upload_expires_at FROM medical_documents WHERE id=$1 AND owner_id=$2`, d.ID, httpx.Owner(r)).Scan(&expectedExpires)
	if err != nil {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	if time.Now().After(expectedExpires) {
		_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='upload_expired',updated_at=now() WHERE id=$1 AND status='initiated'`, d.ID)
		httpx.WriteError(w, 410, "UPLOAD_EXPIRED", "The upload URL has expired. Create a new upload intent.", nil)
		return
	}
	info, err := m.Objects.Stat(r.Context(), d.ObjectKey)
	if err != nil {
		httpx.WriteError(w, 409, "UPLOAD_NOT_FOUND", "The uploaded file is not present in private storage.", nil)
		return
	}
	if info.Size != d.ByteSize {
		_ = m.Objects.Remove(r.Context(), d.ObjectKey)
		_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='size_mismatch',updated_at=now() WHERE id=$1`, d.ID)
		httpx.WriteError(w, 422, "UPLOAD_SIZE_MISMATCH", "The uploaded file size does not match the declared size.", nil)
		return
	}
	object, err := m.Objects.Get(r.Context(), d.ObjectKey)
	if err != nil {
		httpx.WriteError(w, 502, "OBJECT_STORAGE_UNAVAILABLE", "Private object storage is unavailable.", nil)
		return
	}
	hash := sha256.New()
	var prefix bytes.Buffer
	_, readErr := io.Copy(io.MultiWriter(hash, &prefix), io.LimitReader(object, 512))
	if readErr == nil {
		_, readErr = io.Copy(hash, io.LimitReader(object, maxDocumentBytes+1))
	}
	closeErr := object.Close()
	if readErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != strings.TrimSpace(d.SHA256) {
		_ = m.Objects.Remove(r.Context(), d.ObjectKey)
		_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='checksum_mismatch',updated_at=now() WHERE id=$1`, d.ID)
		httpx.WriteError(w, 422, "UPLOAD_CHECKSUM_MISMATCH", "The uploaded file checksum does not match.", nil)
		return
	}
	if actualType := http.DetectContentType(prefix.Bytes()); actualType != d.ContentType {
		_ = m.Objects.Remove(r.Context(), d.ObjectKey)
		_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='content_type_mismatch',updated_at=now() WHERE id=$1`, d.ID)
		httpx.WriteError(w, 415, "CONTENT_TYPE_MISMATCH", "The uploaded file content does not match its declared type.", nil)
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "UPLOAD_COMPLETE_FAILED", "The upload could not be completed.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `UPDATE medical_documents SET status='queued',updated_at=now() WHERE id=$1 AND owner_id=$2 AND status='initiated'`, d.ID, httpx.Owner(r))
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('document.process','medical_document',$1::uuid,jsonb_build_object('document_id',$1::uuid::text))`, d.ID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.upload_completed','medical_document',$3)`, httpx.Owner(r), d.ProfileID, d.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "UPLOAD_COMPLETE_FAILED", "The upload could not be completed.", nil)
		return
	}
	d.Status = "queued"
	httpx.WriteJSON(w, 200, d)
}

func (m *Module) documentDownloadURL(w http.ResponseWriter, r *http.Request) {
	d, ok := m.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok || d.Status == "deleted" || d.Status == "deleting" {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	url, err := m.Objects.PresignedGet(r.Context(), d.ObjectKey, maxDownloadURLAge)
	if err != nil {
		httpx.WriteError(w, 502, "OBJECT_STORAGE_UNAVAILABLE", "Private object storage is unavailable.", nil)
		return
	}
	if _, err = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.download_url_created','medical_document',$3)`, httpx.Owner(r), d.ProfileID, d.ID); err != nil {
		httpx.WriteError(w, 500, "AUDIT_WRITE_FAILED", "Document access could not be recorded.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"document_id": d.ID, "download_url": url.String(), "expires_at": time.Now().UTC().Add(maxDownloadURLAge)})
}

func (m *Module) documentVersions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	var exists bool
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM medical_documents WHERE id=$1 AND owner_id=$2)`, id, httpx.Owner(r)).Scan(&exists); err != nil || !exists {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `WITH RECURSIVE versions AS (
		SELECT id,profile_id,owner_id,file_name,content_type,byte_size,status,document_type,document_version,supersedes_document_id,created_at FROM medical_documents WHERE id=$1 AND owner_id=$2
		UNION ALL
		SELECT d.id,d.profile_id,d.owner_id,d.file_name,d.content_type,d.byte_size,d.status,d.document_type,d.document_version,d.supersedes_document_id,d.created_at FROM medical_documents d JOIN versions v ON d.supersedes_document_id=v.id WHERE d.owner_id=$2
	) SELECT id,profile_id,file_name,content_type,byte_size,status,document_type,document_version,supersedes_document_id,created_at FROM versions WHERE status NOT IN ('deleting','deleted') ORDER BY document_version`, id, httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "DOCUMENT_VERSIONS_UNAVAILABLE", "Document versions are unavailable.", nil)
		return
	}
	defer rows.Close()
	type version struct {
		ID                   string    `json:"id"`
		ProfileID            string    `json:"profile_id"`
		FileName             string    `json:"file_name"`
		ContentType          string    `json:"content_type"`
		ByteSize             int64     `json:"byte_size"`
		Status               string    `json:"status"`
		DocumentType         string    `json:"document_type"`
		DocumentVersion      int       `json:"document_version"`
		SupersedesDocumentID *string   `json:"supersedes_document_id,omitempty"`
		CreatedAt            time.Time `json:"created_at"`
	}
	items := []version{}
	for rows.Next() {
		var item version
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.FileName, &item.ContentType, &item.ByteSize, &item.Status, &item.DocumentType, &item.DocumentVersion, &item.SupersedesDocumentID, &item.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "DOCUMENT_VERSIONS_UNAVAILABLE", "Document versions are unavailable.", nil)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		httpx.WriteError(w, 500, "DOCUMENT_VERSIONS_UNAVAILABLE", "Document versions are unavailable.", nil)
		return
	}
	httpx.WriteJSON(w, 200, items)
}

func (m *Module) documentExtractionPages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	if _, ok := m.documentForOwner(r, id); !ok {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT page_number,extracted_text FROM document_extraction_pages WHERE document_id=$1 ORDER BY page_number LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "EXTRACTION_UNAVAILABLE", "Extracted pages are unavailable.", nil)
		return
	}
	defer rows.Close()
	type page struct {
		PageNumber int    `json:"page_number"`
		Text       string `json:"text"`
	}
	items := []page{}
	for rows.Next() {
		var item page
		if err := rows.Scan(&item.PageNumber, &item.Text); err != nil {
			httpx.WriteError(w, 500, "EXTRACTION_UNAVAILABLE", "Extracted pages are unavailable.", nil)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		httpx.WriteError(w, 500, "EXTRACTION_UNAVAILABLE", "Extracted pages are unavailable.", nil)
		return
	}
	if _, err = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) SELECT $1,profile_id,'document.extraction_accessed','medical_document',$2::uuid::text FROM medical_documents WHERE id=$2 AND owner_id=$1`, httpx.Owner(r), id); err != nil {
		httpx.WriteError(w, 500, "AUDIT_WRITE_FAILED", "Document access could not be recorded.", nil)
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	httpx.WriteJSON(w, 200, items)
}

type documentPatch struct {
	Filename     *string `json:"filename"`
	DocumentType *string `json:"document_type"`
}

func (m *Module) updateDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := m.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	var in documentPatch
	if httpx.DecodeJSON(r, 8<<10, &in) != nil || (in.Filename == nil && in.DocumentType == nil) {
		httpx.WriteError(w, 400, "INVALID_DOCUMENT_UPDATE", "Provide a filename or document type.", nil)
		return
	}
	filename := d.FileName
	if in.Filename != nil {
		filename = filepath.Base(strings.TrimSpace(*in.Filename))
		if filename == "." || filename == ".." || filename == "" || len(filename) > 255 {
			httpx.WriteError(w, 400, "INVALID_FILENAME", "The filename is invalid.", nil)
			return
		}
	}
	docType := d.DocumentType
	if in.DocumentType != nil {
		docType = strings.ToUpper(strings.TrimSpace(*in.DocumentType))
		if !validMedicalDocumentType(docType) {
			httpx.WriteError(w, 400, "INVALID_DOCUMENT_TYPE", "The document type is invalid.", nil)
			return
		}
	}
	previousName, previousType := d.FileName, d.DocumentType
	err := m.DB.Pool.QueryRow(r.Context(), `UPDATE medical_documents SET file_name=$3,document_type=$4,updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING file_name,document_type,updated_at`, d.ID, httpx.Owner(r), filename, docType).Scan(&d.FileName, &d.DocumentType, &d.UpdatedAt)
	if err != nil {
		httpx.WriteError(w, 500, "DOCUMENT_UPDATE_FAILED", "The document could not be updated.", nil)
		return
	}
	// Previous values are kept so the change can be reviewed and reversed from the document history.
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'document.metadata_updated','medical_document',$3,jsonb_build_object('previous_file_name',$4::text,'file_name',$5::text,'previous_document_type',$6::text,'document_type',$7::text))`, httpx.Owner(r), d.ProfileID, d.ID, previousName, d.FileName, previousType, d.DocumentType)
	httpx.WriteJSON(w, 200, d)
}

func (m *Module) deleteDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := m.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		httpx.WriteError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	if d.Status != "deleting" {
		tx, err := m.DB.Pool.Begin(r.Context())
		if err != nil {
			httpx.WriteError(w, 500, "DOCUMENT_DELETE_FAILED", "The document could not be deleted.", nil)
			return
		}
		defer tx.Rollback(r.Context())
		_, err = tx.Exec(r.Context(), `UPDATE medical_documents SET status='deleting',updated_at=now() WHERE id=$1 AND owner_id=$2`, d.ID, httpx.Owner(r))
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.deletion_requested','medical_document',$3)`, httpx.Owner(r), d.ProfileID, d.ID)
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			httpx.WriteError(w, 500, "DOCUMENT_DELETE_FAILED", "The document could not be deleted.", nil)
			return
		}
	}
	if err := m.Objects.Remove(r.Context(), d.ObjectKey); err != nil {
		httpx.WriteError(w, 502, "DOCUMENT_DELETE_PENDING", "The document is queued for deletion retry.", nil)
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "DOCUMENT_DELETE_PENDING", "The document is queued for deletion retry.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `DELETE FROM observations WHERE document_id=$1 AND profile_id=$2 AND owner_id=$3`, d.ID, d.ProfileID, httpx.Owner(r))
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM medical_documents WHERE id=$1 AND owner_id=$2 AND status='deleting'`, d.ID, httpx.Owner(r))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.deleted','medical_document',$3)`, httpx.Owner(r), d.ProfileID, d.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "DOCUMENT_DELETE_PENDING", "The document is queued for deletion retry.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) documentForOwner(r *http.Request, id string) (document, bool) {
	var d document
	var extraction []byte
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT id,profile_id,object_key,file_name,content_type,byte_size,status,document_type,COALESCE(sha256,''),document_version,supersedes_document_id,attempt_count,error_code,extraction,created_at,updated_at FROM medical_documents WHERE id=$1 AND owner_id=$2`, id, httpx.Owner(r)).Scan(&d.ID, &d.ProfileID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.DocumentType, &d.SHA256, &d.DocumentVersion, &d.SupersedesDocumentID, &d.AttemptCount, &d.ErrorCode, &extraction, &d.CreatedAt, &d.UpdatedAt)
	d.Extraction = extraction
	return d, err == nil
}

func (m *Module) hasDocumentConsents(ctx context.Context, profileID string) bool {
	return (m.HasConsent(ctx, profileID, "medical_record_storage") || m.HasConsent(ctx, profileID, "health_data")) && (m.HasConsent(ctx, profileID, "document_processing") || m.HasConsent(ctx, profileID, "ai_processing"))
}

func validDocumentType(value string) bool {
	return value == "application/pdf" || value == "image/jpeg" || value == "image/png"
}

func validMedicalDocumentType(value string) bool {
	switch strings.ToUpper(value) {
	case "LAB_REPORT", "PRESCRIPTION", "DISCHARGE_SUMMARY", "RADIOLOGY_REPORT", "MEDICAL_CERTIFICATE", "VACCINATION_RECORD", "OTHER":
		return true
	}
	return false
}
