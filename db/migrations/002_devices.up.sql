CREATE TABLE IF NOT EXISTS devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    device_name     TEXT NOT NULL,
    os              TEXT NOT NULL CHECK (os IN ('darwin', 'windows', 'linux')),
    agent_version   TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'active', 'suspended', 'decommissioned')),
    enrolled_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (org_id, device_name)
);

CREATE INDEX IF NOT EXISTS idx_devices_org_id ON devices(org_id);
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);
