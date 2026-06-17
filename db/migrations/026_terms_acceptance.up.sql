ALTER TABLE admin_users
  ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ;
