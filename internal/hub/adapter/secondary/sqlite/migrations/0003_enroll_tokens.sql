CREATE TABLE enroll_tokens (
    token_hash TEXT    PRIMARY KEY,
    expires_at INTEGER NOT NULL,
    used_at    INTEGER
);
