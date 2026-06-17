CREATE TABLE IF NOT EXISTS enrollment_short_codes (
    code        TEXT PRIMARY KEY,
    device_id   UUID NOT NULL REFERENCES devices(id),
    org_id      UUID NOT NULL REFERENCES organizations(id),
    config_json JSONB NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_enrollment_short_codes_expires
    ON enrollment_short_codes(expires_at);
