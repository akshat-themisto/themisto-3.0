-- Seed default admin user: admin@themisto.dev / changeme123
-- Password hash is bcrypt of "changeme123"
INSERT INTO admin_users (id, org_id, email, name, password_hash, role)
VALUES (
    'b0000000-0000-0000-0000-000000000001',
    'a0000000-0000-0000-0000-000000000001',
    'admin@themisto.dev',
    'Admin',
    '$2y$10$W.aArRMlZBXQgj0jNDXHoOzTgnu.JjSGYO1gRBj7qFyqBGVqqfkmS',
    'owner'
) ON CONFLICT (id) DO NOTHING;
