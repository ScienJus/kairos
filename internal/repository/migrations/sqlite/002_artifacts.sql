CREATE TABLE artifact_blobs (
    uri TEXT PRIMARY KEY,
    digest TEXT NOT NULL,
    size INTEGER NOT NULL CHECK (size >= 0),
    created_at_ns INTEGER NOT NULL
);
-- +kairos StatementBreak
CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items (id),
    task_id TEXT NOT NULL REFERENCES tasks (id),
    claim_id TEXT NOT NULL REFERENCES claims (id),
    submission_id TEXT,
    name TEXT NOT NULL,
    uri TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    UNIQUE (submission_id, name)
);
-- +kairos StatementBreak
CREATE INDEX artifacts_work_item_time_idx ON artifacts (work_item_id, created_at_ns, id);
-- +kairos StatementBreak
CREATE INDEX artifacts_task_time_idx ON artifacts (task_id, created_at_ns, id);
-- +kairos StatementBreak
CREATE INDEX artifacts_claim_submission_idx ON artifacts (claim_id, submission_id, id);
-- +kairos StatementBreak
CREATE INDEX artifacts_gc_idx
    ON artifacts (submission_id, created_at_ns, claim_id, id);
-- +kairos StatementBreak
CREATE INDEX artifacts_uri_idx ON artifacts (uri);
