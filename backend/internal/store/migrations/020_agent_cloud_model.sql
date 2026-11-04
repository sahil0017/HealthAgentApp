-- The agent may use a cloud model only when the user grants this permission (off by default).
ALTER TABLE agent_permissions DROP CONSTRAINT IF EXISTS agent_permissions_permission_check;
ALTER TABLE agent_permissions ADD CONSTRAINT agent_permissions_permission_check
    CHECK (permission IN ('read_labs','read_devices','read_lifestyle','propose_actions','cloud_model'));
