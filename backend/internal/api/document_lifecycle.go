package api

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

func (a *API) createUploadIntent(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	if !a.hasDocumentConsents(r.Context(), profileID) {
		writeError(w, 403, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	var in uploadIntentInput
	if decodeJSON(r, 16<<10, &in) != nil {
		writeError(w, 400, "INVALID_UPLOAD_INTENT", "The upload request is invalid.", nil)
		return
	}
	filename := filepath.Base(strings.TrimSpace(in.Filename))
	contentType, _, contentTypeErr := mime.ParseMediaType(in.ContentType)
	if contentTypeErr != nil || filename == "." || filename == "" || len(filename) > 255 || in.SizeBytes < 1 || in.SizeBytes > maxDocumentBytes || !validDocumentType(contentType) || !validMedicalDocumentType(in.DocumentType) {
		writeError(w, 400, "INVALID_UPLOAD_INTENT", "The filename, type, document category, or size is invalid.", nil)
		return
	}
	documentVersion := 1
	if in.SupersedesDocumentID != nil {
		var previousVersion int
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT document_version FROM medical_documents WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status NOT IN ('deleting','deleted')`, *in.SupersedesDocumentID, profileID, owner(r)).Scan(&previousVersion); err != nil {
			writeError(w, 404, "SOURCE_DOCUMENT_NOT_FOUND", "The document being replaced could not be found.", nil)
			return
		}
		documentVersion = previousVersion + 1
	}
	checksum, err := hex.DecodeString(strings.TrimSpace(in.SHA256))
	if err != nil || len(checksum) != sha256.Size {
		writeError(w, 400, "INVALID_CHECKSUM", "SHA-256 must be 64 hexadecimal characters.", map[string]string{"sha256": "invalid"})
		return
	}
	sha256Hex := strings.ToLower(strings.TrimSpace(in.SHA256))
	var duplicateID string
	err = a.db.Pool.QueryRow(r.Context(), `SELECT id FROM medical_documents WHERE profile_id=$1 AND sha256=$2 AND status NOT IN ('deleting','deleted') ORDER BY created_at LIMIT 1`, profileID, sha256Hex).Scan(&duplicateID)
	if err == nil {
		writeError(w, http.StatusConflict, "DUPLICATE_DOCUMENT", "This file is already stored for this profile.", map[string]string{"document_id": duplicateID})
		return
	}
	if err != pgx.ErrNoRows {
		writeError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		writeError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	key := profileID + "/" + hex.EncodeToString(random[:])
	expires := time.Now().UTC().Add(uploadIntentLifetime)
	uploadURL, err := a.objects.PresignedPut(r.Context(), key, uploadIntentLifetime)
	if err != nil {
		writeError(w, 502, "OBJECT_STORAGE_UNAVAILABLE", "Private object storage is unavailable.", nil)
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var documentID string
	err = tx.QueryRow(r.Context(), `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,document_type,sha256,status,upload_expires_at,document_version,supersedes_document_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'initiated',$9,$10,$11) RETURNING id`, profileID, owner(r), key, filename, contentType, in.SizeBytes, strings.ToUpper(in.DocumentType), sha256Hex, expires, documentVersion, in.SupersedesDocumentID).Scan(&documentID)
	if err != nil {
		var existingID string
		if lookupErr := a.db.Pool.QueryRow(r.Context(), `SELECT id FROM medical_documents WHERE profile_id=$1 AND sha256=$2 AND status NOT IN ('deleting','deleted') ORDER BY created_at LIMIT 1`, profileID, sha256Hex).Scan(&existingID); lookupErr == nil {
			writeError(w, http.StatusConflict, "DUPLICATE_DOCUMENT", "This file is already stored for this profile.", map[string]string{"document_id": existingID})
			return
		}
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.upload_intent_created','medical_document',$3)`, owner(r), profileID, documentID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "UPLOAD_INTENT_FAILED", "The upload intent could not be created.", nil)
		return
	}
	writeJSON(w, http.StatusCreated, uploadIntentResponse{DocumentID: documentID, UploadURL: uploadURL.String(), ExpiresAt: expires, Status: "INITIATED", DocumentVersion: documentVersion, SupersedesDocumentID: in.SupersedesDocumentID})
}

func (a *API) completeUpload(w http.ResponseWriter, r *http.Request) {
	d, ok := a.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	if !a.hasDocumentConsents(r.Context(), d.ProfileID) {
		writeError(w, 403, "CONSENT_REQUIRED", "Document storage and processing consent are required.", nil)
		return
	}
	if d.Status != "initiated" {
		writeJSON(w, 200, d)
		return
	}
	var expectedExpires time.Time
	err := a.db.Pool.QueryRow(r.Context(), `SELECT upload_expires_at FROM medical_documents WHERE id=$1 AND owner_id=$2`, d.ID, owner(r)).Scan(&expectedExpires)
	if err != nil {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	if time.Now().After(expectedExpires) {
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='upload_expired',updated_at=now() WHERE id=$1 AND status='initiated'`, d.ID)
		writeError(w, 410, "UPLOAD_EXPIRED", "The upload URL has expired. Create a new upload intent.", nil)
		return
	}
	info, err := a.objects.Stat(r.Context(), d.ObjectKey)
	if err != nil {
		writeError(w, 409, "UPLOAD_NOT_FOUND", "The uploaded file is not present in private storage.", nil)
		return
	}
	if info.Size != d.ByteSize {
		_ = a.objects.Remove(r.Context(), d.ObjectKey)
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='size_mismatch',updated_at=now() WHERE id=$1`, d.ID)
		writeError(w, 422, "UPLOAD_SIZE_MISMATCH", "The uploaded file size does not match the declared size.", nil)
		return
	}
	object, err := a.objects.Get(r.Context(), d.ObjectKey)
	if err != nil {
		writeError(w, 502, "OBJECT_STORAGE_UNAVAILABLE", "Private object storage is unavailable.", nil)
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
		_ = a.objects.Remove(r.Context(), d.ObjectKey)
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='checksum_mismatch',updated_at=now() WHERE id=$1`, d.ID)
		writeError(w, 422, "UPLOAD_CHECKSUM_MISMATCH", "The uploaded file checksum does not match.", nil)
		return
	}
	if actualType := http.DetectContentType(prefix.Bytes()); actualType != d.ContentType {
		_ = a.objects.Remove(r.Context(), d.ObjectKey)
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE medical_documents SET status='failed',error_code='content_type_mismatch',updated_at=now() WHERE id=$1`, d.ID)
		writeError(w, 415, "CONTENT_TYPE_MISMATCH", "The uploaded file content does not match its declared type.", nil)
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "UPLOAD_COMPLETE_FAILED", "The upload could not be completed.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `UPDATE medical_documents SET status='queued',updated_at=now() WHERE id=$1 AND owner_id=$2 AND status='initiated'`, d.ID, owner(r))
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('document.process','medical_document',$1::uuid,jsonb_build_object('document_id',$1::uuid::text))`, d.ID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.upload_completed','medical_document',$3)`, owner(r), d.ProfileID, d.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "UPLOAD_COMPLETE_FAILED", "The upload could not be completed.", nil)
		return
	}
	d.Status = "queued"
	writeJSON(w, 200, d)
}

func (a *API) documentDownloadURL(w http.ResponseWriter, r *http.Request) {
	d, ok := a.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok || d.Status == "deleted" || d.Status == "deleting" {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	url, err := a.objects.PresignedGet(r.Context(), d.ObjectKey, maxDownloadURLAge)
	if err != nil {
		writeError(w, 502, "OBJECT_STORAGE_UNAVAILABLE", "Private object storage is unavailable.", nil)
		return
	}
	if _, err = a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.download_url_created','medical_document',$3)`, owner(r), d.ProfileID, d.ID); err != nil {
		writeError(w, 500, "AUDIT_WRITE_FAILED", "Document access could not be recorded.", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"document_id": d.ID, "download_url": url.String(), "expires_at": time.Now().UTC().Add(maxDownloadURLAge)})
}

func (a *API) documentVersions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	var exists bool
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM medical_documents WHERE id=$1 AND owner_id=$2)`, id, owner(r)).Scan(&exists); err != nil || !exists {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `WITH RECURSIVE versions AS (
		SELECT id,profile_id,owner_id,file_name,content_type,byte_size,status,document_type,document_version,supersedes_document_id,created_at FROM medical_documents WHERE id=$1 AND owner_id=$2
		UNION ALL
		SELECT d.id,d.profile_id,d.owner_id,d.file_name,d.content_type,d.byte_size,d.status,d.document_type,d.document_version,d.supersedes_document_id,d.created_at FROM medical_documents d JOIN versions v ON d.supersedes_document_id=v.id WHERE d.owner_id=$2
	) SELECT id,profile_id,file_name,content_type,byte_size,status,document_type,document_version,supersedes_document_id,created_at FROM versions WHERE status NOT IN ('deleting','deleted') ORDER BY document_version`, id, owner(r))
	if err != nil {
		writeError(w, 500, "DOCUMENT_VERSIONS_UNAVAILABLE", "Document versions are unavailable.", nil)
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
			writeError(w, 500, "DOCUMENT_VERSIONS_UNAVAILABLE", "Document versions are unavailable.", nil)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeError(w, 500, "DOCUMENT_VERSIONS_UNAVAILABLE", "Document versions are unavailable.", nil)
		return
	}
	writeJSON(w, 200, items)
}

func (a *API) documentExtractionPages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "documentID")
	if _, ok := a.documentForOwner(r, id); !ok {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	limit, offset, err := page(r)
	if err != nil {
		writeError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT page_number,extracted_text FROM document_extraction_pages WHERE document_id=$1 ORDER BY page_number LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		writeError(w, 500, "EXTRACTION_UNAVAILABLE", "Extracted pages are unavailable.", nil)
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
			writeError(w, 500, "EXTRACTION_UNAVAILABLE", "Extracted pages are unavailable.", nil)
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeError(w, 500, "EXTRACTION_UNAVAILABLE", "Extracted pages are unavailable.", nil)
		return
	}
	if _, err = a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) SELECT $1,profile_id,'document.extraction_accessed','medical_document',$2::uuid::text FROM medical_documents WHERE id=$2 AND owner_id=$1`, owner(r), id); err != nil {
		writeError(w, 500, "AUDIT_WRITE_FAILED", "Document access could not be recorded.", nil)
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	writeJSON(w, 200, items)
}

type documentPatch struct {
	Filename     *string `json:"filename"`
	DocumentType *string `json:"document_type"`
}

func (a *API) updateDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := a.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	var in documentPatch
	if decodeJSON(r, 8<<10, &in) != nil || (in.Filename == nil && in.DocumentType == nil) {
		writeError(w, 400, "INVALID_DOCUMENT_UPDATE", "Provide a filename or document type.", nil)
		return
	}
	filename := d.FileName
	if in.Filename != nil {
		filename = filepath.Base(strings.TrimSpace(*in.Filename))
		if filename == "." || filename == "" || len(filename) > 255 {
			writeError(w, 400, "INVALID_FILENAME", "The filename is invalid.", nil)
			return
		}
	}
	docType := d.DocumentType
	if in.DocumentType != nil {
		docType = strings.ToUpper(strings.TrimSpace(*in.DocumentType))
		if !validMedicalDocumentType(docType) {
			writeError(w, 400, "INVALID_DOCUMENT_TYPE", "The document type is invalid.", nil)
			return
		}
	}
	err := a.db.Pool.QueryRow(r.Context(), `UPDATE medical_documents SET file_name=$3,document_type=$4,updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING file_name,document_type,updated_at`, d.ID, owner(r), filename, docType).Scan(&d.FileName, &d.DocumentType, &d.UpdatedAt)
	if err != nil {
		writeError(w, 500, "DOCUMENT_UPDATE_FAILED", "The document could not be updated.", nil)
		return
	}
	_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.metadata_updated','medical_document',$3)`, owner(r), d.ProfileID, d.ID)
	writeJSON(w, 200, d)
}

func (a *API) deleteDocument(w http.ResponseWriter, r *http.Request) {
	d, ok := a.documentForOwner(r, chi.URLParam(r, "documentID"))
	if !ok {
		writeError(w, 404, "DOCUMENT_NOT_FOUND", "The requested document could not be found.", nil)
		return
	}
	if d.Status != "deleting" {
		tx, err := a.db.Pool.Begin(r.Context())
		if err != nil {
			writeError(w, 500, "DOCUMENT_DELETE_FAILED", "The document could not be deleted.", nil)
			return
		}
		defer tx.Rollback(r.Context())
		_, err = tx.Exec(r.Context(), `UPDATE medical_documents SET status='deleting',updated_at=now() WHERE id=$1 AND owner_id=$2`, d.ID, owner(r))
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.deletion_requested','medical_document',$3)`, owner(r), d.ProfileID, d.ID)
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			writeError(w, 500, "DOCUMENT_DELETE_FAILED", "The document could not be deleted.", nil)
			return
		}
	}
	if err := a.objects.Remove(r.Context(), d.ObjectKey); err != nil {
		writeError(w, 502, "DOCUMENT_DELETE_PENDING", "The document is queued for deletion retry.", nil)
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "DOCUMENT_DELETE_PENDING", "The document is queued for deletion retry.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `DELETE FROM observations WHERE document_id=$1 AND profile_id=$2 AND owner_id=$3`, d.ID, d.ProfileID, owner(r))
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM medical_documents WHERE id=$1 AND owner_id=$2 AND status='deleting'`, d.ID, owner(r))
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.deleted','medical_document',$3)`, owner(r), d.ProfileID, d.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "DOCUMENT_DELETE_PENDING", "The document is queued for deletion retry.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) documentForOwner(r *http.Request, id string) (document, bool) {
	var d document
	var extraction []byte
	err := a.db.Pool.QueryRow(r.Context(), `SELECT id,profile_id,object_key,file_name,content_type,byte_size,status,document_type,sha256,document_version,supersedes_document_id,attempt_count,error_code,extraction,created_at,updated_at FROM medical_documents WHERE id=$1 AND owner_id=$2`, id, owner(r)).Scan(&d.ID, &d.ProfileID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.Status, &d.DocumentType, &d.SHA256, &d.DocumentVersion, &d.SupersedesDocumentID, &d.AttemptCount, &d.ErrorCode, &extraction, &d.CreatedAt, &d.UpdatedAt)
	d.Extraction = extraction
	return d, err == nil
}

func (a *API) hasDocumentConsents(ctx context.Context, profileID string) bool {
	return (a.hasConsent(ctx, profileID, "medical_record_storage") || a.hasConsent(ctx, profileID, "health_data")) && (a.hasConsent(ctx, profileID, "document_processing") || a.hasConsent(ctx, profileID, "ai_processing"))
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
