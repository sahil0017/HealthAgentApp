-- Prescriptions: medicines a person confirmed from a doctor's prescription, with a general daily schedule.
-- The photo is never stored; every line was reviewed by the person before it was saved.
CREATE TABLE IF NOT EXISTS prescriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    doctor TEXT CHECK (doctor IS NULL OR char_length(doctor) <= 160),
    prescribed_on DATE,
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','photo')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT prescriptions_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS prescriptions_profile_idx ON prescriptions(profile_id, created_at DESC);

CREATE TABLE IF NOT EXISTS prescription_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    prescription_id UUID NOT NULL REFERENCES prescriptions(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    strength TEXT NOT NULL DEFAULT '' CHECK (char_length(strength) <= 80),
    form TEXT NOT NULL DEFAULT '' CHECK (char_length(form) <= 60),
    dose TEXT NOT NULL DEFAULT '' CHECK (char_length(dose) <= 80),
    frequency_text TEXT NOT NULL DEFAULT '' CHECK (char_length(frequency_text) <= 120),
    timing_text TEXT NOT NULL DEFAULT '' CHECK (char_length(timing_text) <= 120),
    duration_text TEXT NOT NULL DEFAULT '' CHECK (char_length(duration_text) <= 80),
    instructions TEXT NOT NULL DEFAULT '' CHECK (char_length(instructions) <= 500),
    slots TEXT[] NOT NULL DEFAULT '{}',
    with_food TEXT NOT NULL DEFAULT 'any' CHECK (with_food IN ('any','before','after','with','empty')),
    as_needed BOOLEAN NOT NULL DEFAULT false,
    duration_days INTEGER CHECK (duration_days IS NULL OR duration_days BETWEEN 1 AND 730),
    ends_on DATE,
    confidence DOUBLE PRECISION CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1),
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT prescription_items_slots_valid CHECK (slots <@ ARRAY['morning','afternoon','evening','night']::text[])
);
CREATE INDEX IF NOT EXISTS prescription_items_profile_idx ON prescription_items(profile_id, active);
CREATE INDEX IF NOT EXISTS prescription_items_rx_idx ON prescription_items(prescription_id);
