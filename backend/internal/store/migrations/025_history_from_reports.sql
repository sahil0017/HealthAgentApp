-- Medical history found in reports is saved in the background after a report is approved, and each
-- condition can exist once per profile however it was added.
ALTER TABLE history_entries
  ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('user','report')),
  ADD COLUMN IF NOT EXISTS source_document_id UUID REFERENCES medical_documents(id) ON DELETE SET NULL;

-- Keep the oldest of any duplicates (same wording ignoring case and punctuation).
DELETE FROM history_entries h USING history_entries k
WHERE h.profile_id=k.profile_id AND h.kind=k.kind
  AND btrim(regexp_replace(lower(h.title),'[^a-z0-9]+',' ','g'))=btrim(regexp_replace(lower(k.title),'[^a-z0-9]+',' ','g'))
  AND (h.created_at, h.id::text) > (k.created_at, k.id::text);

CREATE UNIQUE INDEX IF NOT EXISTS history_entries_unique_idx
  ON history_entries (profile_id, kind, (btrim(regexp_replace(lower(title),'[^a-z0-9]+',' ','g'))));

CREATE TABLE IF NOT EXISTS history_scans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL UNIQUE REFERENCES medical_documents(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','done','skipped','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    run_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS history_scans_pending_idx ON history_scans (run_after) WHERE status IN ('pending','running');
