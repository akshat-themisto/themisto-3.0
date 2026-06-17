CREATE TABLE IF NOT EXISTS certificates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID NOT NULL REFERENCES devices(id),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    serial          TEXT NOT NULL UNIQUE,
    subject_cn      TEXT NOT NULL,
    subject_o       TEXT NOT NULL,
    issuer_cn       TEXT NOT NULL,
    key_algorithm   TEXT NOT NULL,
    not_before      TIMESTAMPTZ NOT NULL,
    not_after       TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'revoked', 'expired')),
    revoked_at      TIMESTAMPTZ,
    revocation_reason TEXT,
    csr_pem         TEXT NOT NULL,
    cert_pem        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_certificates_device_id ON certificates(device_id);
CREATE INDEX IF NOT EXISTS idx_certificates_org_id ON certificates(org_id);
CREATE INDEX IF NOT EXISTS idx_certificates_serial ON certificates(serial);
CREATE INDEX IF NOT EXISTS idx_certificates_status ON certificates(status);
CREATE INDEX IF NOT EXISTS idx_certificates_not_after ON certificates(not_after)
    WHERE status = 'active';
