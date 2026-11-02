CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_sessions (
    token_hash CHAR(64) PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS api_sessions_user_idx ON api_sessions(user_id, expires_at DESC);

ALTER TABLE profiles ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'UTC';
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS measurement_system TEXT NOT NULL DEFAULT 'metric';
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_measurement_system_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_measurement_system_check CHECK (measurement_system IN ('metric','us_customary'));

ALTER TABLE consents ADD COLUMN IF NOT EXISTS policy_version TEXT NOT NULL DEFAULT '1';
ALTER TABLE consents ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE consents ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;
ALTER TABLE consents DROP CONSTRAINT IF EXISTS consents_purpose_check;
ALTER TABLE consents ADD CONSTRAINT consents_purpose_check CHECK (purpose IN (
    'health_data','ai_processing','research','medical_record_storage','document_processing',
    'health_data_integration','camera_observation_storage','research_data_access','data_sharing','notifications'
));

ALTER TABLE medical_documents ADD COLUMN IF NOT EXISTS document_type TEXT NOT NULL DEFAULT 'OTHER';
ALTER TABLE medical_documents ADD COLUMN IF NOT EXISTS sha256 CHAR(64);
ALTER TABLE medical_documents ADD COLUMN IF NOT EXISTS upload_expires_at TIMESTAMPTZ;
ALTER TABLE medical_documents DROP CONSTRAINT IF EXISTS medical_documents_status_check;
ALTER TABLE medical_documents ADD CONSTRAINT medical_documents_status_check CHECK (status IN (
    'initiated','queued','processing','review_required','manual_review_required','approved','failed','deleting','deleted'
));
CREATE INDEX IF NOT EXISTS medical_documents_queue_idx ON medical_documents(status, retry_after, created_at);

CREATE TABLE IF NOT EXISTS biomarker_catalog (
    code TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    synonyms TEXT[] NOT NULL DEFAULT '{}',
    supported_units TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO biomarker_catalog(code,display_name,synonyms,supported_units) VALUES
    ('HBA1C','Hemoglobin A1c',ARRAY['HbA1c','A1c'],ARRAY['%','mmol/mol']),
    ('GLUCOSE','Glucose',ARRAY['Blood glucose','Fasting glucose'],ARRAY['mg/dL','mmol/L']),
    ('LDL_C','LDL cholesterol',ARRAY['LDL'],ARRAY['mg/dL','mmol/L']),
    ('HDL_C','HDL cholesterol',ARRAY['HDL'],ARRAY['mg/dL','mmol/L']),
    ('CREATININE','Creatinine',ARRAY['Serum creatinine'],ARRAY['mg/dL','µmol/L']),
    ('TSH','Thyroid stimulating hormone',ARRAY['Thyrotropin'],ARRAY['mIU/L','µIU/mL'])
ON CONFLICT(code) DO NOTHING;

ALTER TABLE observations ADD COLUMN IF NOT EXISTS biomarker_code TEXT REFERENCES biomarker_catalog(code);
ALTER TABLE observations ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'LAB_DOCUMENT';
ALTER TABLE observations ADD COLUMN IF NOT EXISTS verification_status TEXT NOT NULL DEFAULT 'VERIFIED';
ALTER TABLE observations ADD COLUMN IF NOT EXISTS revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE observations ADD COLUMN IF NOT EXISTS supersedes_id UUID REFERENCES observations(id);
CREATE INDEX IF NOT EXISTS observations_history_idx ON observations(profile_id, biomarker_code, observed_at DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS observation_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    observation_id UUID NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    snapshot JSONB NOT NULL,
    change_reason TEXT NOT NULL DEFAULT '',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (observation_id, revision),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
