CREATE TABLE IF NOT EXISTS outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','dead_letter','cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    lock_owner TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS outbox_claim_idx ON outbox_events(status, available_at, created_at);
CREATE INDEX IF NOT EXISTS outbox_aggregate_idx ON outbox_events(aggregate_type, aggregate_id, created_at DESC);

CREATE TABLE IF NOT EXISTS document_extraction_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES medical_documents(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    pipeline_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('succeeded','manual_review_required','failed')),
    page_count INTEGER NOT NULL DEFAULT 0,
    warning TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS document_extraction_runs_doc_idx ON document_extraction_runs(document_id, created_at DESC);

CREATE TABLE IF NOT EXISTS document_extraction_pages (
    document_id UUID NOT NULL REFERENCES medical_documents(id) ON DELETE CASCADE,
    page_number INTEGER NOT NULL CHECK (page_number > 0),
    extracted_text TEXT NOT NULL,
    PRIMARY KEY (document_id, page_number)
);

CREATE TABLE IF NOT EXISTS document_extraction_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES medical_documents(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    biomarker_code TEXT REFERENCES biomarker_catalog(code),
    display_name TEXT NOT NULL,
    value_numeric NUMERIC NOT NULL,
    unit TEXT NOT NULL,
    observed_at DATE,
    reference_range TEXT NOT NULL DEFAULT '',
    source_page INTEGER NOT NULL CHECK (source_page > 0),
    evidence TEXT NOT NULL,
    review_status TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending','accepted','rejected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS extraction_candidates_doc_idx ON document_extraction_candidates(document_id, review_status);

ALTER TABLE medical_documents ADD COLUMN IF NOT EXISTS document_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE medical_documents ADD COLUMN IF NOT EXISTS supersedes_document_id UUID REFERENCES medical_documents(id) ON DELETE SET NULL;
ALTER TABLE medical_documents ADD COLUMN IF NOT EXISTS malware_scan_status TEXT NOT NULL DEFAULT 'not_configured'
    CHECK (malware_scan_status IN ('not_configured','pending','clean','rejected','error'));
CREATE UNIQUE INDEX IF NOT EXISTS medical_documents_profile_sha_unique
    ON medical_documents(profile_id, sha256)
    WHERE sha256 IS NOT NULL AND status NOT IN ('deleting','deleted');
