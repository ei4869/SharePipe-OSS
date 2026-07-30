ALTER TABLE upload_tasks ADD COLUMN completion_session_id TEXT;

ALTER TABLE shares ADD COLUMN cleanup_error TEXT;
ALTER TABLE shares ADD COLUMN cleanup_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE shares ADD COLUMN cleanup_next_attempt_at INTEGER;

CREATE INDEX shares_cleanup_retry_idx
    ON shares(status, cleanup_next_attempt_at, updated_at);
