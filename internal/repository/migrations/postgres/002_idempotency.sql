CREATE TABLE idempotency_records (
    actor_kind TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    operation_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response JSONB NOT NULL,
    created_at_ns BIGINT NOT NULL,
    PRIMARY KEY (actor_kind, actor_id, operation_id)
);
