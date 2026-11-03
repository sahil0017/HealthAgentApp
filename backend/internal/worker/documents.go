package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"strings"
	"time"

	"cortex/backend/internal/knowledge"
	"cortex/backend/internal/objects"
	"cortex/backend/internal/processing"
	"cortex/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	maxAttempts     = 5
	leaseDuration   = 2 * time.Minute
	processingLimit = 15 << 20
	maxTextPreview  = 12000
)

type DocumentWorker struct {
	DB        *store.Store
	Objects   *objects.Store
	Processor processing.DocumentProcessor
	ID        string
	Interval  time.Duration
	Knowledge *knowledge.Service
}

func NewDocumentWorker(db *store.Store, objectStore *objects.Store) *DocumentWorker {
	w := &DocumentWorker{DB: db, Objects: objectStore, Knowledge: knowledge.NewService(db), ID: fmt.Sprintf("documents-%d", time.Now().UnixNano()), Interval: time.Second}
	w.Processor = processing.PDFTextProcessor{Catalog: w.loadCatalog}
	return w
}

func (w *DocumentWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		processed, err := w.ProcessOne(ctx)
		if err != nil {
			log.Printf("document worker: %v", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *DocumentWorker) ProcessOne(ctx context.Context) (bool, error) {
	tx, err := w.DB.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var eventID, documentID string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,aggregate_id::text,attempts+1 FROM outbox_events WHERE event_type='document.process' AND ((status='pending' AND available_at<=now()) OR (status='processing' AND locked_at<$1)) ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1`, time.Now().Add(-leaseDuration)).Scan(&eventID, &documentID, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE outbox_events SET status='processing',attempts=$2,locked_at=now(),lock_owner=$3,updated_at=now() WHERE id=$1`, eventID, attempts, w.ID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	err = w.process(ctx, eventID, documentID)
	if err != nil {
		if retryErr := w.retry(ctx, eventID, documentID, attempts, err); retryErr != nil {
			return true, errors.Join(err, retryErr)
		}
		return true, err
	}
	return true, nil
}

type documentInput struct {
	ID          string
	ProfileID   string
	OwnerID     string
	ObjectKey   string
	FileName    string
	ContentType string
	ByteSize    int64
	SHA256      string
	Status      string
}

func (w *DocumentWorker) process(ctx context.Context, eventID, documentID string) error {
	var d documentInput
	err := w.DB.Pool.QueryRow(ctx, `SELECT id,profile_id,owner_id,object_key,file_name,content_type,byte_size,COALESCE(sha256,''),status FROM medical_documents WHERE id=$1`, documentID).Scan(&d.ID, &d.ProfileID, &d.OwnerID, &d.ObjectKey, &d.FileName, &d.ContentType, &d.ByteSize, &d.SHA256, &d.Status)
	if err != nil {
		return err
	}
	if d.Status == "review_required" || d.Status == "manual_review_required" || d.Status == "approved" {
		_, err = w.DB.Pool.Exec(ctx, `UPDATE outbox_events SET status='completed',locked_at=NULL,lock_owner=NULL,updated_at=now() WHERE id=$1`, eventID)
		return err
	}
	var consent bool
	err = w.DB.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM consents WHERE profile_id=$1 AND purpose IN ('document_processing','ai_processing','health_data') AND granted AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())) AND EXISTS(SELECT 1 FROM consents WHERE profile_id=$1 AND purpose IN ('medical_record_storage','health_data') AND granted AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()))`, d.ProfileID).Scan(&consent)
	if err != nil {
		return err
	}
	if !consent {
		return w.cancelForConsent(ctx, eventID, d)
	}
	if d.ByteSize < 1 || d.ByteSize > processingLimit {
		return fmt.Errorf("document size outside processing limit")
	}
	object, err := w.Objects.Get(ctx, d.ObjectKey)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(object, processingLimit+1))
	closeErr := object.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if int64(len(data)) != d.ByteSize {
		return fmt.Errorf("object size changed after upload validation")
	}
	if d.SHA256 != "" {
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != strings.TrimSpace(d.SHA256) {
			return fmt.Errorf("object checksum changed after upload validation")
		}
	}
	processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := w.Processor.Process(processCtx, d.ContentType, data)
	if err != nil {
		return err
	}
	if err := w.persist(ctx, eventID, d, result); err != nil {
		return err
	}
	// Unmatched labels become research tasks; the proposals still need human approval before the catalog changes.
	for _, name := range result.UnknownNames {
		if err := w.Knowledge.Enqueue(ctx, "biomarker", name); err != nil {
			log.Printf("queue research for %q: %v", name, err)
		}
	}
	return nil
}

// loadCatalog reads approved analytes; learned units only widen what the extractor recognises, never validation.
func (w *DocumentWorker) loadCatalog(ctx context.Context) []processing.CatalogEntry {
	rows, err := w.DB.Pool.Query(ctx, `SELECT code,display_name,synonyms,supported_units||learned_units FROM biomarker_catalog WHERE status IN ('approved','legacy_seed')`)
	if err != nil {
		log.Printf("load catalog: %v", err)
		return nil
	}
	defer rows.Close()
	var out []processing.CatalogEntry
	for rows.Next() {
		var e processing.CatalogEntry
		if rows.Scan(&e.Code, &e.Name, &e.Terms, &e.Units) == nil {
			out = append(out, e)
		}
	}
	return out
}

func (w *DocumentWorker) persist(ctx context.Context, eventID string, d documentInput, result processing.Result) error {
	status := "review_required"
	runStatus := "succeeded"
	if result.ManualReview {
		status, runStatus = "manual_review_required", "manual_review_required"
	}
	if len(result.Warnings) == 0 {
		result.Warnings = []string{}
	}
	if len(result.Candidates) == 0 {
		result.Candidates = []processing.Candidate{}
	}
	tx, err := w.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM document_extraction_pages WHERE document_id=$1`, d.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM document_extraction_candidates WHERE document_id=$1`, d.ID); err != nil {
		return err
	}
	for i, page := range result.Pages {
		if _, err = tx.Exec(ctx, `INSERT INTO document_extraction_pages(document_id,page_number,extracted_text) VALUES($1,$2,$3)`, d.ID, i+1, page); err != nil {
			return err
		}
	}
	for i := range result.Candidates {
		candidate := &result.Candidates[i]
		if err = tx.QueryRow(ctx, `INSERT INTO document_extraction_candidates(document_id,profile_id,owner_id,biomarker_code,display_name,value_numeric,unit,observed_at,reference_range,source_page,evidence) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,NULLIF($8,'')::date,$9,$10,$11) RETURNING id::text`, d.ID, d.ProfileID, d.OwnerID, candidate.BiomarkerCode, candidate.Name, candidate.Value, candidate.Unit, stringValue(candidate.ObservedAt), candidate.ReferenceRange, candidate.Page, candidate.Evidence).Scan(&candidate.ID); err != nil {
			return err
		}
	}
	textPreview := result.Text
	if len(textPreview) > maxTextPreview {
		textPreview = textPreview[:maxTextPreview]
	}
	classification := processing.Classify(result.Text)
	extraction, err := json.Marshal(map[string]any{"classification": classification, "biomarkers": result.Candidates, "text_preview": textPreview, "warnings": result.Warnings, "pipeline_version": processing.PipelineVersion, "page_count": result.PageCount})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO document_extraction_runs(document_id,profile_id,pipeline_version,status,page_count,warning) VALUES($1,$2,$3,$4,$5,$6)`, d.ID, d.ProfileID, processing.PipelineVersion, runStatus, result.PageCount, strings.Join(result.Warnings, " ")); err != nil {
		return err
	}
	// A suggested type only fills in an unset type; an owner's choice is never overwritten.
	if _, err = tx.Exec(ctx, `UPDATE medical_documents SET status=$2,extraction=$3::jsonb,document_type=CASE WHEN document_type='OTHER' THEN $4 ELSE document_type END,error_code=NULL,retry_after=NULL,updated_at=now() WHERE id=$1 AND status IN ('queued','processing')`, d.ID, status, string(extraction), classification.Type); err != nil {
		return err
	}
	if classification.Type != "OTHER" {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) SELECT $1,$2,'document.type_suggested','medical_document',$3::text,jsonb_build_object('document_type',$4::text,'confidence',$5::float8) WHERE EXISTS(SELECT 1 FROM medical_documents WHERE id=$3::text::uuid AND document_type=$4::text)`, d.OwnerID, d.ProfileID, d.ID, classification.Type, classification.Confidence); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'document.processed','medical_document',$3,jsonb_build_object('pipeline_version',$4::text,'page_count',$5::int,'candidate_count',$6::int,'manual_review',$7::boolean))`, d.OwnerID, d.ProfileID, d.ID, processing.PipelineVersion, result.PageCount, len(result.Candidates), result.ManualReview); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE outbox_events SET status='completed',locked_at=NULL,lock_owner=NULL,last_error=NULL,updated_at=now() WHERE id=$1`, eventID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *DocumentWorker) cancelForConsent(ctx context.Context, eventID string, d documentInput) error {
	tx, err := w.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE medical_documents SET status='failed',error_code='processing_consent_required',updated_at=now() WHERE id=$1 AND status IN ('queued','processing')`, d.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE outbox_events SET status='cancelled',last_error='required consent is not active',locked_at=NULL,lock_owner=NULL,updated_at=now() WHERE id=$1`, eventID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'document.processing_blocked','medical_document',$3,'{"reason":"consent_required"}'::jsonb)`, d.OwnerID, d.ProfileID, d.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *DocumentWorker) retry(ctx context.Context, eventID, documentID string, attempts int, cause error) error {
	var ownerID, profileID string
	_ = w.DB.Pool.QueryRow(ctx, `SELECT owner_id,profile_id FROM medical_documents WHERE id=$1`, documentID).Scan(&ownerID, &profileID)
	tx, err := w.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	if attempts >= maxAttempts {
		if _, err = tx.Exec(ctx, `UPDATE outbox_events SET status='dead_letter',last_error=$2,locked_at=NULL,lock_owner=NULL,updated_at=now() WHERE id=$1`, eventID, message); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE medical_documents SET status='failed',error_code='processing_failed',updated_at=now() WHERE id=$1 AND status IN ('queued','processing')`, documentID); err != nil {
			return err
		}
	} else {
		delay := time.Duration(1<<min(attempts, 8))*time.Second + time.Duration(rand.Int64N(int64(time.Second)))
		if _, err = tx.Exec(ctx, `UPDATE outbox_events SET status='pending',available_at=now()+make_interval(secs=>$2),last_error=$3,locked_at=NULL,lock_owner=NULL,updated_at=now() WHERE id=$1`, eventID, delay.Seconds(), message); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE medical_documents SET status='queued',error_code='processing_retry_scheduled',retry_after=now()+make_interval(secs=>$2),updated_at=now() WHERE id=$1 AND status IN ('queued','processing')`, documentID, delay.Seconds()); err != nil {
			return err
		}
	}
	if ownerID != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'document.processing_attempt_failed','medical_document',$3,jsonb_build_object('attempt',$4::int,'terminal',$5::boolean))`, ownerID, profileID, documentID, attempts, attempts >= maxAttempts); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
