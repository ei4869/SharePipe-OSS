ALTER TABLE shares ADD COLUMN password_version INTEGER NOT NULL DEFAULT 1;

CREATE INDEX download_actions_share_expiry_idx
ON download_actions(share_id, expires_at);

CREATE TABLE upload_tasks (
    share_id TEXT PRIMARY KEY REFERENCES shares(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('uploading','paused','completing','completed','cancel_requested','aborted','expired')),
    cancel_requested_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX upload_tasks_status_idx ON upload_tasks(status, updated_at);

INSERT INTO upload_tasks(share_id,status,cancel_requested_at,created_at,updated_at)
SELECT id,
    CASE
        WHEN status='uploading' THEN 'uploading'
        WHEN status='active' THEN 'completed'
        WHEN status='expired' THEN 'expired'
        ELSE 'aborted'
    END,
    CASE WHEN status='revoked' THEN COALESCE(revoked_at,updated_at) ELSE NULL END,
    created_at,updated_at
FROM shares;
