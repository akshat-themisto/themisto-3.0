ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS request_body_encrypted BYTEA,
    ADD COLUMN IF NOT EXISTS request_body_nonce BYTEA,
    ADD COLUMN IF NOT EXISTS request_body_truncated BOOLEAN NOT NULL DEFAULT FALSE;
