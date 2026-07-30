ALTER TABLE invite_codes ADD COLUMN kind TEXT NOT NULL DEFAULT 'standard'
    CHECK(kind IN ('standard', 'trusted_device'));
ALTER TABLE invite_codes ADD COLUMN device_name TEXT NOT NULL DEFAULT '';

ALTER TABLE shares ADD COLUMN invite_id TEXT;
ALTER TABLE shares ADD COLUMN trusted_device_id TEXT;
ALTER TABLE shares ADD COLUMN pairing_enabled INTEGER NOT NULL DEFAULT 0 CHECK(pairing_enabled IN (0, 1));
ALTER TABLE shares ADD COLUMN qr_enabled INTEGER NOT NULL DEFAULT 0 CHECK(qr_enabled IN (0, 1));

CREATE TABLE trusted_devices (
    id TEXT PRIMARY KEY,
    credential_hash TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    source_invite_id TEXT,
    platform TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    first_ip_digest TEXT,
    last_ip_digest TEXT,
    expires_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    revoked_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX trusted_devices_active_idx ON trusted_devices(revoked_at, expires_at, last_seen_at DESC);

CREATE TABLE pair_codes (
    share_id TEXT PRIMARY KEY REFERENCES shares(id) ON DELETE CASCADE,
    code_digest TEXT NOT NULL UNIQUE,
    code_encrypted TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX pair_codes_active_idx ON pair_codes(expires_at, revoked_at);

CREATE TABLE share_access_tokens (
    id TEXT PRIMARY KEY,
    share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    source TEXT NOT NULL CHECK(source IN ('pairing')),
    expires_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX share_access_tokens_lookup_idx ON share_access_tokens(token_hash, expires_at);
CREATE INDEX share_access_tokens_share_idx ON share_access_tokens(share_id, expires_at);

INSERT OR IGNORE INTO settings(key, value, updated_at)
VALUES('pair_code_ttl_seconds', '600', unixepoch());
