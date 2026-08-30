CREATE TABLE coordination_claims (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    kind TEXT NOT NULL CHECK (kind IN ('empty_blackboard', 'blackboard_completion', 'work_item_acceptance')),
    executor_kind TEXT NOT NULL CHECK (executor_kind = 'agent'),
    executor_id TEXT NOT NULL,
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    claimed_at TEXT NOT NULL,
    last_heartbeat_at TEXT NOT NULL,
    lease_until TEXT NOT NULL,
    lease_seconds INTEGER NOT NULL CHECK (lease_seconds > 0),
    updated_at TEXT NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload))
);

-- +kairos StatementBreak
CREATE UNIQUE INDEX coordination_claims_one_active_idx
    ON coordination_claims (work_item_id) WHERE active = 1;

-- +kairos StatementBreak
CREATE INDEX coordination_claims_work_item_time_idx
    ON coordination_claims (work_item_id, claimed_at, id);

-- +kairos StatementBreak
CREATE INDEX coordination_claims_active_lease_idx
    ON coordination_claims (active, lease_until, work_item_id);
