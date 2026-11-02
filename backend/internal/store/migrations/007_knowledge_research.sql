-- Catalog rows now carry provenance; rows learned by the research agent start life as proposals in knowledge_entries.
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'approved';
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS loinc_nums TEXT[] NOT NULL DEFAULT '{}';
-- learned_units only hint the extractor; supported_units remains the validation list so learning never restricts users.
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS learned_units TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS reference_note JSONB;
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS approved_by TEXT;
ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;

-- Rows seeded by migration 003 are no longer trusted as prefilled data. Unreferenced ones are removed;
-- ones already used by observations are kept but flagged for re-verification by the research agent.
DELETE FROM biomarker_catalog c
WHERE c.source = 'manual' AND c.created_at = (SELECT min(created_at) FROM biomarker_catalog)
  AND c.code IN ('HBA1C','GLUCOSE','LDL_C','HDL_C','CREATININE','TSH')
  AND NOT EXISTS (SELECT 1 FROM observations o WHERE o.biomarker_code = c.code);
UPDATE biomarker_catalog SET status = 'legacy_seed', source = 'legacy_seed'
WHERE code IN ('HBA1C','GLUCOSE','LDL_C','HDL_C','CREATININE','TSH') AND source = 'manual';

CREATE TABLE IF NOT EXISTS knowledge_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('biomarker','unit','research_source','reference_note')),
    entry_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
    agreement INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    proposed_by TEXT NOT NULL DEFAULT 'research-agent',
    reviewed_by TEXT,
    review_note TEXT,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, entry_key)
);
CREATE INDEX IF NOT EXISTS knowledge_entries_status_idx ON knowledge_entries(status, kind, created_at);

CREATE TABLE IF NOT EXISTS research_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('discover','biomarker','unit','literature')),
    term TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','done','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, term)
);
CREATE INDEX IF NOT EXISTS research_tasks_claim_idx ON research_tasks(status, available_at, created_at);

-- Approved literature, chunked and embedded for retrieval. Contains public abstracts only, never user data.
CREATE TABLE IF NOT EXISTS research_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_entry_id UUID NOT NULL UNIQUE REFERENCES knowledge_entries(id) ON DELETE CASCADE,
    identifier TEXT NOT NULL,
    title TEXT NOT NULL,
    journal TEXT,
    published_year INTEGER,
    url TEXT NOT NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS research_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID NOT NULL REFERENCES research_sources(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL,
    content TEXT NOT NULL,
    embedding vector(384) NOT NULL,
    embedding_model TEXT NOT NULL,
    content_tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
    UNIQUE (source_id, ordinal)
);
CREATE INDEX IF NOT EXISTS research_chunks_embedding_idx ON research_chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS research_chunks_tsv_idx ON research_chunks USING gin (content_tsv);
