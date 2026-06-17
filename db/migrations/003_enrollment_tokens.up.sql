CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID NOT NULL REFERENCES devices(id),
    token_hash      TEXT NOT NULL UNIQUE,
    used            BOOLEAN NOT NULL DEFAULT false,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_device_id ON enrollment_tokens(device_id);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_expires ON enrollment_tokens(expires_at)
    WHERE used = false;
