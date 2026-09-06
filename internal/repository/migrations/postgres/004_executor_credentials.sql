ALTER TABLE claims ADD COLUMN executor_token_hash TEXT;

-- +kairos StatementBreak
CREATE UNIQUE INDEX claims_executor_token_hash_idx
    ON claims (executor_token_hash) WHERE executor_token_hash IS NOT NULL;

-- +kairos StatementBreak
ALTER TABLE coordination_claims ADD COLUMN executor_token_hash TEXT;

-- +kairos StatementBreak
CREATE UNIQUE INDEX coordination_claims_executor_token_hash_idx
    ON coordination_claims (executor_token_hash) WHERE executor_token_hash IS NOT NULL;
