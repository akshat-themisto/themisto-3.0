-- Add renewal token hash to devices for certificate auto-renewal.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS renewal_token_hash TEXT;

-- Add 'superseded' status to certificates for renewal tracking.
ALTER TABLE certificates DROP CONSTRAINT IF EXISTS certificates_status_check;
ALTER TABLE certificates ADD CONSTRAINT certificates_status_check
    CHECK (status IN ('active', 'revoked', 'expired', 'superseded'));
