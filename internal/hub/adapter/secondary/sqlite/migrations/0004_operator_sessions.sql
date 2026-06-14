CREATE TABLE operator_sessions (
    id           TEXT PRIMARY KEY,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER
);
CREATE INDEX idx_operator_sessions_expires ON operator_sessions(expires_at);
