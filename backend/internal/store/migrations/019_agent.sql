-- Module 19 Agent: permissions the user grants the assistant, and the actions it proposes. The agent only
-- ever proposes; nothing is created in the user's record until the user approves it.
CREATE TABLE IF NOT EXISTS agent_permissions (
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read_labs','read_devices','read_lifestyle','propose_actions')),
    granted BOOLEAN NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_id, permission),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id UUID NOT NULL,
    owner_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('follow_up')),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    reason TEXT NOT NULL CHECK (char_length(reason) <= 500),
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','approved','rejected')),
    result_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at TIMESTAMPTZ,
    UNIQUE (profile_id, dedupe_key),
    FOREIGN KEY (profile_id, owner_id) REFERENCES profiles(id, owner_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS agent_actions_profile_idx ON agent_actions(profile_id, status, created_at DESC);
