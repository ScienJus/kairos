CREATE TABLE idempotency_records (
    actor_kind TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response TEXT NOT NULL CHECK (json_valid(response)),
    created_at_ns INTEGER NOT NULL,
    PRIMARY KEY (actor_kind, actor_id, operation_id)
);
