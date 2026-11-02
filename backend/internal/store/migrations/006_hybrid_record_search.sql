ALTER TABLE record_chunks ADD COLUMN IF NOT EXISTS content_tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED;
CREATE INDEX IF NOT EXISTS record_chunks_content_tsv_idx ON record_chunks USING gin (content_tsv);
CREATE INDEX IF NOT EXISTS record_chunks_model_idx ON record_chunks(embedding_model);
