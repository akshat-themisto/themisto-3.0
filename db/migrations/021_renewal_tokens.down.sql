ALTER TABLE devices DROP COLUMN IF EXISTS renewal_token_hash;

ALTER TABLE certificates DROP CONSTRAINT IF EXISTS certificates_status_check;
ALTER TABLE certificates ADD CONSTRAINT certificates_status_check
    CHECK (status IN ('active', 'revoked', 'expired'));
