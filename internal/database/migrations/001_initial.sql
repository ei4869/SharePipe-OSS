CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE invite_codes (
    id TEXT PRIMARY KEY,
    code_hash TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL DEFAULT '',
    max_uses INTEGER,
    uses INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER,
    revoked_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX invite_codes_active_idx ON invite_codes(expires_at, revoked_at);

CREATE TABLE shares (
    id TEXT PRIMARY KEY,
    public_token_hash TEXT NOT NULL UNIQUE,
    manage_token_hash TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    password_hash TEXT,
    storage_backend TEXT NOT NULL CHECK(storage_backend IN ('local', 's3')),
    status TEXT NOT NULL CHECK(status IN ('uploading', 'active', 'revoked', 'expired', 'cleanup_pending', 'metadata_only', 'deleted')),
    expires_at INTEGER NOT NULL,
    download_limit INTEGER,
    download_count INTEGER NOT NULL DEFAULT 0,
    file_count INTEGER NOT NULL DEFAULT 0,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    content_deleted_at INTEGER,
    metadata_delete_after INTEGER,
    revoked_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX shares_lifecycle_idx ON shares(status, expires_at, metadata_delete_after);
CREATE INDEX shares_created_idx ON shares(created_at DESC);

CREATE TABLE entries (
    id TEXT PRIMARY KEY,
    share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('file', 'directory')),
    size INTEGER NOT NULL DEFAULT 0,
    mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    storage_key TEXT,
    sha256 TEXT,
    status TEXT NOT NULL CHECK(status IN ('pending', 'ready', 'quarantined', 'deleted')),
    created_at INTEGER NOT NULL,
    UNIQUE(share_id, path)
);
CREATE INDEX entries_share_path_idx ON entries(share_id, path);

CREATE TABLE upload_sessions (
    id TEXT PRIMARY KEY,
    share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    backend TEXT NOT NULL CHECK(backend IN ('local', 's3')),
    expected_size INTEGER NOT NULL,
    chunk_size INTEGER NOT NULL,
    expected_parts INTEGER NOT NULL,
    received_bytes INTEGER NOT NULL DEFAULT 0,
    s3_upload_id TEXT,
    status TEXT NOT NULL CHECK(status IN ('open', 'assembling', 'completed', 'aborted', 'expired')),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX upload_sessions_expiry_idx ON upload_sessions(status, expires_at);

CREATE TABLE upload_parts (
    session_id TEXT NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
    part_number INTEGER NOT NULL,
    size INTEGER NOT NULL,
    checksum TEXT,
    etag TEXT,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(session_id, part_number)
);

CREATE TABLE download_actions (
    id TEXT PRIMARY KEY,
    share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    entry_id TEXT REFERENCES entries(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('file', 'zip')),
    token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(status IN ('ready', 'preparing', 'failed')),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX download_actions_token_idx ON download_actions(token_hash, expires_at);

CREATE TABLE zip_objects (
    share_id TEXT PRIMARY KEY REFERENCES shares(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK(status IN ('preparing', 'ready', 'failed')),
    expires_at INTEGER,
    error TEXT,
    updated_at INTEGER NOT NULL
);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('queued', 'running', 'succeeded', 'failed')),
    run_at INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    lease_until INTEGER,
    last_error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX jobs_claim_idx ON jobs(state, run_at, lease_until);

CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id TEXT,
    action TEXT NOT NULL,
    result TEXT NOT NULL,
    ip_digest TEXT,
    full_ip TEXT,
    user_agent TEXT,
    details TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX audit_logs_created_idx ON audit_logs(created_at);
CREATE INDEX audit_logs_share_idx ON audit_logs(share_id, created_at);

INSERT INTO settings(key, value, updated_at) VALUES
    ('diagnostic_until', 'null', unixepoch());
