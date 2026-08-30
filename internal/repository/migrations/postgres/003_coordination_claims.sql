CREATE TABLE coordination_claims (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    kind TEXT NOT NULL CHECK (kind IN ('empty_blackboard', 'blackboard_completion', 'work_item_acceptance')),
    executor_kind TEXT NOT NULL CHECK (executor_kind = 'agent'),
    executor_id TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    last_heartbeat_at TIMESTAMPTZ NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    lease_seconds BIGINT NOT NULL CHECK (lease_seconds > 0),
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);

-- +kairos StatementBreak
CREATE UNIQUE INDEX coordination_claims_one_active_idx
    ON coordination_claims (work_item_id) WHERE active;

-- +kairos StatementBreak
CREATE INDEX coordination_claims_work_item_time_idx
    ON coordination_claims (work_item_id, claimed_at, id);

-- +kairos StatementBreak
CREATE INDEX coordination_claims_active_lease_idx
    ON coordination_claims (active, lease_until, work_item_id);
