CREATE TABLE daemon_instances (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    registered_at TEXT NOT NULL,
    last_report_at TEXT NOT NULL,
    stopped_at TEXT,
    last_revision INTEGER NOT NULL DEFAULT 0 CHECK (last_revision >= 0),
    lifecycle TEXT NOT NULL CHECK (lifecycle IN ('running', 'stopping', 'stopped')),
    health TEXT NOT NULL CHECK (health IN ('unknown', 'healthy', 'paused')),
    active_count INTEGER NOT NULL DEFAULT 0 CHECK (active_count >= 0),
    payload TEXT NOT NULL CHECK (json_valid(payload))
);
-- +kairos StatementBreak
CREATE INDEX daemon_instances_recent_idx ON daemon_instances (last_report_at DESC, id);
-- +kairos StatementBreak
CREATE INDEX daemon_instances_agent_recent_idx ON daemon_instances (agent_id, last_report_at DESC, id);
-- +kairos StatementBreak
CREATE TABLE daemon_events (
    instance_id TEXT NOT NULL REFERENCES daemon_instances (id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    kind TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    received_at TEXT NOT NULL,
    work_item_id TEXT,
    task_id TEXT,
    claim_id TEXT,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    PRIMARY KEY (instance_id, sequence)
);
-- +kairos StatementBreak
CREATE INDEX daemon_events_received_idx ON daemon_events (received_at, instance_id, sequence);
