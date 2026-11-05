-- Live-discovered sources (papers, trials, plain-language guides) kept with each followed question.
DO $$
DECLARE c TEXT;
BEGIN
  FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='research_investigation_items'::regclass AND contype='c' LOOP
    EXECUTE format('ALTER TABLE research_investigation_items DROP CONSTRAINT %I', c);
  END LOOP;
END $$;
ALTER TABLE research_investigation_items
  ADD COLUMN IF NOT EXISTS web_title TEXT,
  ADD COLUMN IF NOT EXISTS web_url TEXT,
  ADD COLUMN IF NOT EXISTS web_publisher TEXT,
  ADD COLUMN IF NOT EXISTS web_year INTEGER,
  ADD COLUMN IF NOT EXISTS web_kind TEXT;
ALTER TABLE research_investigation_items
  ADD CONSTRAINT research_items_kind_check CHECK (kind IN ('source','note','web')),
  ADD CONSTRAINT research_items_excerpt_check CHECK (excerpt IS NULL OR char_length(excerpt) <= 2000),
  ADD CONSTRAINT research_items_note_check CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 2000),
  ADD CONSTRAINT research_items_shape_check CHECK (
    (kind='source' AND source_id IS NOT NULL) OR (kind='note' AND note IS NOT NULL) OR (kind='web' AND web_url IS NOT NULL AND web_title IS NOT NULL));
CREATE UNIQUE INDEX IF NOT EXISTS research_investigation_web_idx ON research_investigation_items(investigation_id, web_url) WHERE web_url IS NOT NULL;
