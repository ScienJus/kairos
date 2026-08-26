CREATE TABLE definitions (
    id TEXT NOT NULL,
    version BIGINT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('workflow', 'blackboard')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    PRIMARY KEY (id, version, mode)
);
-- +kairos StatementBreak
CREATE TABLE work_items (
    id TEXT PRIMARY KEY,
    definition_id TEXT NOT NULL,
    definition_version BIGINT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('workflow', 'blackboard')),
    status TEXT NOT NULL,
    acceptance_mode TEXT NOT NULL DEFAULT 'none' CHECK (acceptance_mode IN ('none', 'agent', 'human')),
    tags TEXT[] NOT NULL DEFAULT '{}',
    version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    FOREIGN KEY (definition_id, definition_version, mode)
        REFERENCES definitions (id, version, mode)
);
-- +kairos StatementBreak
CREATE INDEX work_items_status_idx ON work_items (status, mode, updated_at, id);
-- +kairos StatementBreak
CREATE INDEX work_items_order_idx ON work_items (updated_at DESC, id);
-- +kairos StatementBreak
CREATE INDEX definitions_list_idx ON definitions (mode, id, version DESC);
-- +kairos StatementBreak
CREATE INDEX work_items_tags_idx ON work_items USING GIN (tags);
-- +kairos StatementBreak
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    parent_task_id TEXT REFERENCES tasks (id),
    status TEXT NOT NULL,
    executor TEXT NOT NULL CHECK (executor IN ('agent', 'human', 'either')),
    allowed_roles TEXT[] NOT NULL DEFAULT '{}',
    tags TEXT[] NOT NULL DEFAULT '{}',
    active_claim_id TEXT,
    position BIGINT NOT NULL,
    version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    UNIQUE (work_item_id, id)
);
-- +kairos StatementBreak
CREATE INDEX tasks_work_item_position_idx ON tasks (work_item_id, position, id);
-- +kairos StatementBreak
CREATE INDEX tasks_status_idx ON tasks (status, id);
-- +kairos StatementBreak
CREATE INDEX tasks_allowed_roles_idx ON tasks USING GIN (allowed_roles);
-- +kairos StatementBreak
CREATE INDEX tasks_tags_idx ON tasks USING GIN (tags);
-- +kairos StatementBreak
CREATE INDEX tasks_parent_position_idx
    ON tasks (work_item_id, parent_task_id, position, id);
-- +kairos StatementBreak
CREATE TABLE task_relations (
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    from_task_id TEXT NOT NULL,
    to_task_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
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
    active BOOLEAN NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    last_heartbeat_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    lease_seconds BIGINT,
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);
-- +kairos StatementBreak
CREATE INDEX claims_task_time_idx ON claims (task_id, claimed_at, id);
-- +kairos StatementBreak
CREATE INDEX claims_active_lease_idx ON claims (active, lease_until, task_id);
-- +kairos StatementBreak
CREATE TABLE workflow_activations (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    workflow_task_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL
);
-- +kairos StatementBreak
CREATE INDEX workflow_activations_lookup_idx
    ON workflow_activations (work_item_id, workflow_task_id, correlation_id, status, created_at, id);
-- +kairos StatementBreak
CREATE TABLE work_item_events (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    sequence BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    UNIQUE (work_item_id, sequence)
);
-- +kairos StatementBreak
CREATE INDEX work_item_events_order_idx
    ON work_item_events (work_item_id, sequence);
-- +kairos StatementBreak
CREATE TABLE idempotency_records (
    actor_kind TEXT NOT NULL,
    actor_id TEXT NOT NULL,
	operation_id TEXT NOT NULL,
	operation TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('pending', 'completed')),
	request_hash TEXT NOT NULL,
    response JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (actor_kind, actor_id, operation_id)
);
-- +kairos StatementBreak
CREATE INDEX idempotency_records_pending_gc_idx
    ON idempotency_records (status, created_at, actor_kind, actor_id, operation_id);
-- +kairos StatementBreak
CREATE INDEX idempotency_records_artifact_gc_idx
    ON idempotency_records (operation, status, updated_at);
-- +kairos StatementBreak
CREATE TABLE identities (
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('human', 'agent')),
    actor_id TEXT NOT NULL,
    role TEXT NOT NULL,
    token_hash TEXT UNIQUE,
    version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (actor_kind, actor_id),
    CHECK (
        (actor_kind = 'human' AND role = '') OR
        (actor_kind = 'agent' AND role <> '')
    )
);
-- +kairos StatementBreak
CREATE INDEX identities_order_idx ON identities (actor_kind, actor_id);
