# 02 Medical Intelligence Vault — 🟢 feature-complete (pending build verification)

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

- [x] Semantic + keyword hybrid search (pgvector + FTS, reciprocal rank fusion); requires `ai_processing` consent, else keyword only — `api/document_semantic.go`, migration 010
- [x] Scanned-PDF OCR via pdftoppm + Tesseract (first 10 pages) — `processing/ocr.go`
- [x] Document change history — `GET /documents/{id}/history`, `api/document_history.go`
- [x] Type-suggestion audit event; edits audit old and new values
- [x] OpenAPI contract updated
- [x] iOS: search all reports, document detail (edit, versions, pages, history) — `ios/CortexApp/DocumentVault.swift`
- [x] Derived embeddings are deleted with their document (tested)

## Remaining gaps (verification, not features)
- [ ] Worker image (Tesseract/poppler) not built in Docker yet
- [ ] iOS code not compiled locally; verify via GitHub Actions `ios-simulator`
- [ ] Classification is keyword-based by design; OCR is English unless `CORTEX_OCR_LANGS` + language packs are set

## Tests
`go test ./internal/processing/ ./internal/worker/ ./internal/api/ -run 'Classify|OCR|Image|Persist|DocumentSearch'`
DB-backed tests need `TEST_DATABASE_URL` pointing at a scratch database.
