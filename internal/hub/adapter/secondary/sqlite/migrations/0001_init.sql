-- one row per enrolled agent
CREATE TABLE machines (
    id            TEXT    PRIMARY KEY,
    name          TEXT    NOT NULL,
    os            TEXT    NOT NULL,
    arch          TEXT,
    agent_version TEXT,
    enrolled_at   INTEGER NOT NULL,
    last_seen_at  INTEGER,
    revoked_at    INTEGER
);

-- long-lived agent credential (M5)
CREATE TABLE machine_credentials (
    machine_id TEXT PRIMARY KEY REFERENCES machines(id),
    public_key BLOB    NOT NULL,
    created_at INTEGER NOT NULL
);

-- logical grouping of sessions, bound to a machine + working dir
CREATE TABLE projects (
    id         TEXT    PRIMARY KEY,
    machine_id TEXT    NOT NULL REFERENCES machines(id),
    name       TEXT    NOT NULL,
    path       TEXT    NOT NULL,
    color      TEXT,
    created_at INTEGER NOT NULL,
    UNIQUE (machine_id, path)
);

-- terminal session metadata (live I/O is NOT here)
CREATE TABLE sessions (
    id             TEXT    PRIMARY KEY,
    project_id     TEXT    REFERENCES projects(id),
    machine_id     TEXT    NOT NULL REFERENCES machines(id),
    title          TEXT,
    shell          TEXT,
    status         TEXT    NOT NULL,
    activity       TEXT,
    exit_code      INTEGER,
    created_at     INTEGER NOT NULL,
    last_active_at INTEGER
);
CREATE INDEX idx_sessions_machine ON sessions(machine_id);
CREATE INDEX idx_sessions_project ON sessions(project_id);

-- security-relevant actions (M5)
CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         INTEGER NOT NULL,
    actor      TEXT    NOT NULL,
    action     TEXT    NOT NULL,
    machine_id TEXT,
    session_id TEXT,
    detail     TEXT
);
CREATE INDEX idx_audit_ts ON audit_log(ts);

-- operator auth (M5): passkey + TOTP + recovery
CREATE TABLE operator_credentials (
    id           TEXT    PRIMARY KEY,
    kind         TEXT    NOT NULL,
    data         BLOB    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
);
