ALTER TABLE dlp_events
    DROP COLUMN IF EXISTS request_body_truncated,
    DROP COLUMN IF EXISTS request_body_nonce,
    DROP COLUMN IF EXISTS request_body_encrypted;
