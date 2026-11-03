-- Device sessions: each login is listable and revocable, and refreshing rotates the token while keeping the original login time.
ALTER TABLE api_sessions ADD COLUMN IF NOT EXISTS id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE api_sessions ADD COLUMN IF NOT EXISTS device TEXT NOT NULL DEFAULT 'api';
ALTER TABLE api_sessions ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE api_sessions ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE api_sessions ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();
UPDATE api_sessions SET started_at = created_at WHERE started_at IS NULL;
ALTER TABLE api_sessions ALTER COLUMN started_at SET NOT NULL;
ALTER TABLE api_sessions ALTER COLUMN started_at SET DEFAULT now();
CREATE UNIQUE INDEX IF NOT EXISTS api_sessions_id_idx ON api_sessions(id);
