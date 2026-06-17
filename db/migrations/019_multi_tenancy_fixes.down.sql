-- Revert to globally unique email.
ALTER TABLE admin_users DROP CONSTRAINT IF EXISTS admin_users_org_email_unique;
ALTER TABLE admin_users ADD CONSTRAINT admin_users_email_key UNIQUE (email);
