CREATE TABLE identities (
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('human', 'agent')),
    actor_id TEXT NOT NULL,
    role TEXT NOT NULL,
    token_hash TEXT UNIQUE,
    version INTEGER NOT NULL,
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (actor_kind, actor_id),
    CHECK (
        (actor_kind = 'human' AND role = '') OR
        (actor_kind = 'agent' AND role <> '')
    )
);
-- +kairos StatementBreak
CREATE INDEX identities_order_idx ON identities (actor_kind, actor_id);
