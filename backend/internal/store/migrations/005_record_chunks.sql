CREATE TABLE IF NOT EXISTS record_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL,
    profile_id UUID NOT NULL,
    observation_id UUID NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    observation_revision INTEGER NOT NULL,
    content TEXT NOT NULL,
    embedding vector(384) NOT NULL,
    embedding_model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (observation_id),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS record_chunks_profile_idx ON record_chunks(profile_id, owner_id);
CREATE INDEX IF NOT EXISTS record_chunks_embedding_idx ON record_chunks USING hnsw (embedding vector_cosine_ops);
