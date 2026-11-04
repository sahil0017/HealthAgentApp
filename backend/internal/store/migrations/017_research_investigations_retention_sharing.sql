-- Module 08: saved research investigations. Sources are only ever approved research sources.
CREATE TABLE IF NOT EXISTS research_investigations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    question TEXT NOT NULL CHECK (char_length(question) BETWEEN 2 AND 500),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_refreshed_at TIMESTAMPTZ,
    CONSTRAINT research_investigations_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS research_investigations_profile_idx ON research_investigations(profile_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS research_investigation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    investigation_id UUID NOT NULL REFERENCES research_investigations(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('source','note')),
    source_id UUID REFERENCES research_sources(id) ON DELETE CASCADE,
    excerpt TEXT CHECK (excerpt IS NULL OR char_length(excerpt) <= 2000),
    note TEXT CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((kind='source' AND source_id IS NOT NULL) OR (kind='note' AND note IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS research_investigation_source_idx ON research_investigation_items(investigation_id, source_id) WHERE source_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS research_investigation_items_idx ON research_investigation_items(investigation_id, created_at);

-- Module 12: retention policy per profile. NULL means keep indefinitely.
CREATE TABLE IF NOT EXISTS retention_policies (
    profile_id UUID PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
    ai_history_days INTEGER CHECK (ai_history_days IS NULL OR ai_history_days BETWEEN 1 AND 3650),
    device_readings_days INTEGER CHECK (device_readings_days IS NULL OR device_readings_days BETWEEN 1 AND 3650),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_applied_at TIMESTAMPTZ
);

-- Applies every retention policy (or one profile's) and returns what it removed. The worker calls it
-- on a schedule; the API calls it for "apply now". Audit events are evidence and are never purged.
CREATE OR REPLACE FUNCTION cortex_apply_retention(target UUID DEFAULT NULL)
RETURNS TABLE(ai_history_removed BIGINT, device_readings_removed BIGINT) AS $$
DECLARE q BIGINT := 0; d BIGINT := 0; n BIGINT;
BEGIN
    DELETE FROM health_questions h USING retention_policies p
     WHERE h.profile_id = p.profile_id AND p.ai_history_days IS NOT NULL
       AND h.created_at < now() - make_interval(days => p.ai_history_days)
       AND (target IS NULL OR p.profile_id = target);
    GET DIAGNOSTICS n = ROW_COUNT; q := n;
    DELETE FROM device_readings r USING retention_policies p
     WHERE r.profile_id = p.profile_id AND p.device_readings_days IS NOT NULL
       AND r.measured_at < now() - make_interval(days => p.device_readings_days)
       AND (target IS NULL OR p.profile_id = target);
    GET DIAGNOSTICS n = ROW_COUNT; d := n;
    UPDATE retention_policies SET last_applied_at = now() WHERE target IS NULL OR profile_id = target;
    RETURN QUERY SELECT q, d;
END $$ LANGUAGE plpgsql;

-- Module 12: family sharing. A share is read-only, scoped, expiring and revocable. The invite code is
-- stored only as a hash; whoever redeems it becomes the grantee.
CREATE TABLE IF NOT EXISTS profile_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    label TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
    scopes TEXT[] NOT NULL CHECK (scopes <@ ARRAY['summary','labs','history','follow_ups']::text[] AND cardinality(scopes) >= 1),
    code_hash TEXT NOT NULL UNIQUE,
    grantee_id TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT profile_shares_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS profile_shares_profile_idx ON profile_shares(profile_id, created_at DESC);
CREATE INDEX IF NOT EXISTS profile_shares_grantee_idx ON profile_shares(grantee_id) WHERE grantee_id IS NOT NULL;
