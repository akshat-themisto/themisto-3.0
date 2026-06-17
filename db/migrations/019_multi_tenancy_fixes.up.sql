-- Fix email uniqueness: scope to org instead of globally unique.
-- This allows the same email to exist in different organizations.
ALTER TABLE admin_users DROP CONSTRAINT IF EXISTS admin_users_email_key;
ALTER TABLE admin_users ADD CONSTRAINT admin_users_org_email_unique UNIQUE (org_id, email);
