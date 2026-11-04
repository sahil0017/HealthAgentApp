-- Module 09 Lifestyle & Symptom: a symptom journal and daily lifestyle logs (sleep, exercise, nutrition,
-- hydration, mood, stress). Self-reported and descriptive; never mixed with verified laboratory results.
CREATE TABLE IF NOT EXISTS symptom_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    severity SMALLINT NOT NULL CHECK (severity BETWEEN 1 AND 10),
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    notes TEXT CHECK (notes IS NULL OR char_length(notes) <= 1000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT symptom_entries_span CHECK (ended_at IS NULL OR ended_at >= started_at),
    CONSTRAINT symptom_entries_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS symptom_entries_profile_idx ON symptom_entries(profile_id, started_at DESC);

CREATE TABLE IF NOT EXISTS lifestyle_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('sleep','exercise','nutrition','hydration','mood','stress')),
    value DOUBLE PRECISION NOT NULL CHECK (value >= 0),
    unit TEXT NOT NULL,
    label TEXT CHECK (label IS NULL OR char_length(label) <= 120),
    notes TEXT CHECK (notes IS NULL OR char_length(notes) <= 1000),
    logged_on DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT lifestyle_logs_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS lifestyle_logs_profile_idx ON lifestyle_logs(profile_id, category, logged_on DESC);
