-- Reference ranges and unit conversions are learned: they arrive only through the reviewed
-- knowledge flow (research agent proposes, a person approves). Nothing is seeded here.
ALTER TABLE knowledge_entries DROP CONSTRAINT IF EXISTS knowledge_entries_kind_check;
ALTER TABLE knowledge_entries ADD CONSTRAINT knowledge_entries_kind_check
    CHECK (kind IN ('biomarker','unit','research_source','reference_note','reference_range','unit_conversion'));

CREATE TABLE IF NOT EXISTS reference_ranges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    biomarker_code TEXT NOT NULL REFERENCES biomarker_catalog(code) ON DELETE CASCADE,
    unit TEXT NOT NULL,
    sex TEXT CHECK (sex IS NULL OR sex IN ('female','male')),
    age_min INTEGER CHECK (age_min IS NULL OR age_min >= 0),
    age_max INTEGER CHECK (age_max IS NULL OR age_max <= 150),
    low NUMERIC,
    high NUMERIC,
    source TEXT NOT NULL,
    knowledge_entry_id UUID REFERENCES knowledge_entries(id) ON DELETE SET NULL,
    approved_by TEXT NOT NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reference_ranges_bounds CHECK ((low IS NOT NULL OR high IS NOT NULL) AND (low IS NULL OR high IS NULL OR low <= high)),
    CONSTRAINT reference_ranges_ages CHECK (age_min IS NULL OR age_max IS NULL OR age_min <= age_max)
);
CREATE UNIQUE INDEX IF NOT EXISTS reference_ranges_unique_idx
    ON reference_ranges(biomarker_code, unit, COALESCE(sex,''), COALESCE(age_min,-1), COALESCE(age_max,-1));

CREATE TABLE IF NOT EXISTS unit_conversions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    biomarker_code TEXT NOT NULL REFERENCES biomarker_catalog(code) ON DELETE CASCADE,
    from_unit TEXT NOT NULL,
    to_unit TEXT NOT NULL,
    factor NUMERIC NOT NULL CHECK (factor > 0),
    "offset" NUMERIC NOT NULL DEFAULT 0,
    source TEXT NOT NULL,
    knowledge_entry_id UUID REFERENCES knowledge_entries(id) ON DELETE SET NULL,
    approved_by TEXT NOT NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT unit_conversions_distinct CHECK (from_unit <> to_unit),
    UNIQUE (biomarker_code, from_unit, to_unit)
);
