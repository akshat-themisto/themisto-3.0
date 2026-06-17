ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS public_backend_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS public_gateway_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status_updated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS status_updated_by TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_organizations_status
    ON organizations(status);
