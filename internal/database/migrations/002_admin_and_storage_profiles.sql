CREATE TABLE admin_emails (
    email TEXT PRIMARY KEY COLLATE NOCASE,
    created_at INTEGER NOT NULL
);

CREATE TABLE storage_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('local', 's3')),
    provider TEXT NOT NULL,
    config_json TEXT NOT NULL,
    credentials_encrypted TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    last_checked_at INTEGER,
    last_error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX storage_profiles_enabled_idx ON storage_profiles(enabled, name);

ALTER TABLE shares ADD COLUMN storage_profile_id TEXT NOT NULL DEFAULT '';
UPDATE shares SET storage_profile_id = storage_backend WHERE storage_profile_id = '';
CREATE INDEX shares_storage_profile_idx ON shares(storage_profile_id, status);

ALTER TABLE upload_sessions ADD COLUMN storage_profile_id TEXT NOT NULL DEFAULT '';
UPDATE upload_sessions SET storage_profile_id = backend WHERE storage_profile_id = '';
CREATE INDEX upload_sessions_storage_profile_idx ON upload_sessions(storage_profile_id, status);

INSERT OR IGNORE INTO settings(key, value, updated_at)
VALUES('admin_password_version', '1', unixepoch());

INSERT OR IGNORE INTO settings(key, value, updated_at)
SELECT 'default_storage_profile', value, unixepoch()
FROM settings WHERE key = 'default_storage_backend';
