DROP INDEX IF EXISTS idx_organizations_status;

ALTER TABLE organizations
    DROP COLUMN IF EXISTS status_updated_by,
    DROP COLUMN IF EXISTS status_updated_at,
    DROP COLUMN IF EXISTS status_reason,
    DROP COLUMN IF EXISTS public_gateway_url,
    DROP COLUMN IF EXISTS public_backend_url;
