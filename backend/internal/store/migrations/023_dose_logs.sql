CREATE TABLE IF NOT EXISTS dose_logs (
    item_id UUID NOT NULL REFERENCES prescription_items(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    day DATE NOT NULL,
    slot TEXT NOT NULL CHECK (slot IN ('morning','afternoon','evening','night')),
    taken_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, day, slot),
    CONSTRAINT dose_logs_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS dose_logs_profile_day_idx ON dose_logs(profile_id, day);
