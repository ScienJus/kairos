-- Existing databases retain their ordinary identities and credential hashes.
ALTER TABLE identities ADD COLUMN credential_source TEXT NOT NULL DEFAULT 'identity'
    CHECK (credential_source IN ('identity', 'admin') AND
           (credential_source <> 'admin' OR (actor_kind = 'human' AND role = '' AND token_hash IS NULL)));

-- +kairos StatementBreak
CREATE UNIQUE INDEX identities_admin_source_idx ON identities (credential_source)
    WHERE credential_source = 'admin';
