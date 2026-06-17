-- Seed organization for the minimal vertical slice.
-- The api_key_hash is a bcrypt hash of the literal string "themisto-dev-key-change-me".
-- On production deploy, generate a real key and update this hash.
INSERT INTO organizations (id, name, slug, api_key_hash, status)
VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'Themisto Dev Org',
    'themisto-dev',
    '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
    'active'
) ON CONFLICT (id) DO NOTHING;
