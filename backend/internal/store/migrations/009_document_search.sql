CREATE INDEX IF NOT EXISTS document_extraction_pages_fts_idx
    ON document_extraction_pages USING GIN (to_tsvector('simple', extracted_text));
