# 02 Medical Intelligence Vault — 🟡 partial

## Done
- [x] Upload (direct and signed upload intents), download, list, delete — `backend/internal/api/documents.go`, `document_lifecycle.go`
- [x] Supported types: PDF, JPEG, PNG
- [x] Document types: lab, prescription, discharge summary, radiology, certificate, vaccination, other
- [x] PDF text extraction and biomarker candidates — `backend/internal/processing/pdf.go`
- [x] Image OCR with Tesseract; falls back to manual review if absent — `processing/ocr.go`
- [x] Document type suggestion from text, never overriding the owner's choice — `processing/classify.go`, `worker/documents.go`
- [x] Cross-document full-text search with document and page citations — `GET /profiles/{id}/documents/search`, `api/document_search.go`
- [x] Versioning (`document_version`, `supersedes_document_id`) and version list — `GET /documents/{id}/versions`
- [x] Human approval before extracted values enter records
- [x] Audit entry for extraction access and search (search text is not logged)

## Not done
- [ ] Classification is keyword-based only; no model-based classifier
- [ ] Search is keyword (Postgres FTS), not semantic; the semantic record search covers observations only
- [ ] Correction history view for documents (observations already support corrections)
- [ ] OCR is English only, and scanned PDFs without a text layer are not OCR'd
- [ ] iOS screens for search and classification
- [ ] Worker image with Tesseract is not yet built or run in Docker (tests use a fake binary)

## Tests
`go test ./internal/processing/ ./internal/worker/ ./internal/api/ -run 'Classify|OCR|Image|Persist|DocumentSearch'`
DB-backed tests need `TEST_DATABASE_URL` pointing at a scratch database.
