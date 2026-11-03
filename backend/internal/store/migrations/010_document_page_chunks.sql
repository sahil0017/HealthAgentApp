CREATE TABLE IF NOT EXISTS document_page_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL,
    profile_id UUID NOT NULL,
    document_id UUID NOT NULL REFERENCES medical_documents(id) ON DELETE CASCADE,
    page_number INTEGER NOT NULL CHECK (page_number > 0),
    chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
    page_sha256 TEXT NOT NULL,
    content TEXT NOT NULL,
    embedding vector(384) NOT NULL,
    embedding_model TEXT NOT NULL,
    content_tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, page_number, chunk_index),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS document_page_chunks_profile_idx ON document_page_chunks(profile_id, owner_id, embedding_model);
CREATE INDEX IF NOT EXISTS document_page_chunks_embedding_idx ON document_page_chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS document_page_chunks_tsv_idx ON document_page_chunks USING gin (content_tsv);
