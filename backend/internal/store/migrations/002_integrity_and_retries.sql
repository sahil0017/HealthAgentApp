ALTER TABLE medical_documents
    ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS retry_after TIMESTAMPTZ;

ALTER TABLE medical_documents
    DROP CONSTRAINT IF EXISTS medical_documents_profile_id_fkey;
ALTER TABLE medical_documents
    DROP CONSTRAINT IF EXISTS medical_documents_profile_id_owner_id_fkey;
ALTER TABLE observations
    DROP CONSTRAINT IF EXISTS observations_profile_id_fkey;
ALTER TABLE observations
    DROP CONSTRAINT IF EXISTS observations_profile_id_owner_id_fkey;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'medical_documents_profile_owner_fkey') THEN
        ALTER TABLE medical_documents ADD CONSTRAINT medical_documents_profile_owner_fkey
            FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'observations_profile_owner_fkey') THEN
        ALTER TABLE observations ADD CONSTRAINT observations_profile_owner_fkey
            FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE;
    END IF;
END $$;
