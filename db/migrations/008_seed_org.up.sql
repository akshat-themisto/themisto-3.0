INSERT INTO organizations (id, name, slug, api_key_hash, status)
VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'Themisto Dev Org',
    'themisto-dev',
    '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
    'active'
) ON CONFLICT (id) DO NOTHING;
