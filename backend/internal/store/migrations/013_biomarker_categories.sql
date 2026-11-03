ALTER TABLE biomarker_catalog ADD COLUMN IF NOT EXISTS category TEXT;
ALTER TABLE biomarker_catalog DROP CONSTRAINT IF EXISTS biomarker_catalog_category_check;
ALTER TABLE biomarker_catalog ADD CONSTRAINT biomarker_catalog_category_check
    CHECK (category IS NULL OR category IN ('cbc','glucose','lipid','liver','kidney','vitamin','hormone','other'));
