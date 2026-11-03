ALTER TABLE research_tasks DROP CONSTRAINT IF EXISTS research_tasks_kind_check;
ALTER TABLE research_tasks ADD CONSTRAINT research_tasks_kind_check
    CHECK (kind IN ('discover','biomarker','unit','literature','conversion'));
