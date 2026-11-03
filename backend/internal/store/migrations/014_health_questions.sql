CREATE TABLE IF NOT EXISTS health_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    question TEXT NOT NULL CHECK (char_length(question) BETWEEN 1 AND 4000),
    answer TEXT NOT NULL,
    sources JSONB NOT NULL DEFAULT '[]',
    limitations JSONB NOT NULL DEFAULT '[]',
    workflow JSONB NOT NULL DEFAULT '[]',
    suggested_questions JSONB NOT NULL DEFAULT '[]',
    urgent BOOLEAN NOT NULL DEFAULT false,
    triage TEXT NOT NULL DEFAULT 'none' CHECK (triage IN ('none','emergency','crisis')),
    confidence TEXT NOT NULL DEFAULT 'insufficient' CHECK (confidence IN ('high','limited','insufficient','not_applicable')),
    uncertainty TEXT NOT NULL DEFAULT '',
    feedback TEXT CHECK (feedback IS NULL OR feedback IN ('helpful','not_helpful')),
    feedback_comment TEXT CHECK (feedback_comment IS NULL OR char_length(feedback_comment) <= 1000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT health_questions_profile_owner_fkey FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS health_questions_profile_created_idx ON health_questions(profile_id, created_at DESC);
