CREATE TABLE definitions (
    id TEXT NOT NULL,
    version INTEGER NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('workflow', 'blackboard')),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    PRIMARY KEY (id, version, mode)
);
-- +kairos StatementBreak
CREATE TABLE work_items (
    id TEXT PRIMARY KEY,
    definition_id TEXT NOT NULL,
    definition_version INTEGER NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('workflow', 'blackboard')),
    status TEXT NOT NULL,
    version INTEGER NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    FOREIGN KEY (definition_id, definition_version, mode)
        REFERENCES definitions (id, version, mode)
);
-- +kairos StatementBreak
CREATE INDEX work_items_status_idx ON work_items (status, id);
-- +kairos StatementBreak
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    status TEXT NOT NULL,
    active_claim_id TEXT,
    position INTEGER NOT NULL,
    version INTEGER NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    UNIQUE (work_item_id, id)
);
-- +kairos StatementBreak
CREATE INDEX tasks_work_item_position_idx ON tasks (work_item_id, position, id);
-- +kairos StatementBreak
CREATE INDEX tasks_status_idx ON tasks (status, id);
-- +kairos StatementBreak
CREATE TABLE task_relations (
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    from_task_id TEXT NOT NULL,
    to_task_id TEXT NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    PRIMARY KEY (work_item_id, from_task_id, to_task_id),
    FOREIGN KEY (work_item_id, from_task_id)
        REFERENCES tasks (work_item_id, id),
    FOREIGN KEY (work_item_id, to_task_id)
        REFERENCES tasks (work_item_id, id)
);
-- +kairos StatementBreak
CREATE INDEX task_relations_to_idx ON task_relations (work_item_id, to_task_id);
-- +kairos StatementBreak
CREATE TABLE claims (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks (id),
    executor_kind TEXT NOT NULL CHECK (executor_kind IN ('agent', 'human')),
    executor_id TEXT NOT NULL,
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    claimed_at_ns INTEGER NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload))
);
-- +kairos StatementBreak
CREATE INDEX claims_task_time_idx ON claims (task_id, claimed_at_ns, id);
-- +kairos StatementBreak
CREATE TABLE workflow_activations (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    workflow_task_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload))
);
-- +kairos StatementBreak
CREATE INDEX workflow_activations_lookup_idx
    ON workflow_activations (work_item_id, workflow_task_id, correlation_id, status, created_at_ns, id);
-- +kairos StatementBreak
CREATE TABLE work_item_events (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    sequence INTEGER NOT NULL,
    occurred_at_ns INTEGER NOT NULL,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    UNIQUE (work_item_id, sequence)
);
-- +kairos StatementBreak
CREATE INDEX work_item_events_order_idx
    ON work_item_events (work_item_id, sequence);
