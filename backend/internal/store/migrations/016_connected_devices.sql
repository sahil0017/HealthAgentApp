-- Module 05 Connected Diagnostics: registered devices, their readings and sync history.
-- Device readings live apart from laboratory observations so the two are never mixed by accident.
CREATE TABLE IF NOT EXISTS devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('phone_health_app','smartwatch','fitness_band','blood_pressure_monitor','glucometer','cgm','scale','pulse_oximeter','thermometer','other')),
    source TEXT NOT NULL CHECK (source IN ('healthkit','bluetooth','import','manual')),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    manufacturer TEXT CHECK (manufacturer IS NULL OR char_length(manufacturer) <= 120),
    model TEXT CHECK (model IS NULL OR char_length(model) <= 120),
    external_id TEXT CHECK (external_id IS NULL OR char_length(external_id) <= 200),
    status TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected','disconnected')),
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT devices_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS devices_external_idx ON devices(profile_id, source, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS devices_profile_idx ON devices(profile_id, created_at DESC);

CREATE TABLE IF NOT EXISTS device_readings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    metric TEXT NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    unit TEXT NOT NULL,
    original_value DOUBLE PRECISION NOT NULL,
    original_unit TEXT NOT NULL,
    measured_at TIMESTAMPTZ NOT NULL,
    local_date DATE NOT NULL,
    dedupe_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT device_readings_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE,
    CONSTRAINT device_readings_dedupe UNIQUE (device_id, dedupe_key)
);
CREATE INDEX IF NOT EXISTS device_readings_metric_idx ON device_readings(profile_id, metric, measured_at DESC);
CREATE INDEX IF NOT EXISTS device_readings_day_idx ON device_readings(profile_id, metric, local_date);

CREATE TABLE IF NOT EXISTS device_syncs (
    id BIGSERIAL PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    accepted INTEGER NOT NULL CHECK (accepted >= 0),
    duplicates INTEGER NOT NULL CHECK (duplicates >= 0),
    rejected INTEGER NOT NULL CHECK (rejected >= 0),
    first_measured_at TIMESTAMPTZ,
    last_measured_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT device_syncs_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS device_syncs_device_idx ON device_syncs(device_id, id DESC);
