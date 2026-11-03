-- Command centre: medical history, goals and follow-ups. All rows belong to a profile and are
-- removed with it, so profile deletion and export stay complete.

CREATE TABLE IF NOT EXISTS history_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL,
    profile_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('condition','allergy','surgery','family_history','medication','vaccination')),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    started_on DATE,
    ended_on DATE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ended_on IS NULL OR started_on IS NULL OR ended_on >= started_on),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS history_entries_profile_idx ON history_entries(profile_id, kind, created_at DESC);

CREATE TABLE IF NOT EXISTS goals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL,
    profile_id UUID NOT NULL,
    biomarker_code TEXT NOT NULL REFERENCES biomarker_catalog(code),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    target_min NUMERIC,
    target_max NUMERIC,
    unit TEXT NOT NULL CHECK (length(unit) BETWEEN 1 AND 40),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (target_min IS NOT NULL OR target_max IS NOT NULL),
    CHECK (target_min IS NULL OR target_max IS NULL OR target_min <= target_max),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS goals_profile_idx ON goals(profile_id, status);

CREATE TABLE IF NOT EXISTS follow_ups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL,
    profile_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('investigation','appointment','medication','other')),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    due_on DATE,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','dismissed')),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS follow_ups_profile_idx ON follow_ups(profile_id, status, due_on);
