ALTER TABLE claims ADD COLUMN last_heartbeat_at_ns BIGINT;
-- +kairos StatementBreak
ALTER TABLE claims ADD COLUMN lease_until_ns BIGINT;
-- +kairos StatementBreak
CREATE INDEX claims_active_lease_idx ON claims (active, lease_until_ns, task_id);
-- +kairos StatementBreak
UPDATE claims SET last_heartbeat_at_ns = claimed_at_ns, lease_until_ns = claimed_at_ns + 60000000000
WHERE active = TRUE AND lease_until_ns IS NULL AND executor_kind = 'agent';
