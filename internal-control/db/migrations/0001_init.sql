CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

DO $$ BEGIN
  CREATE TYPE staff_role AS ENUM ('owner', 'admin', 'operator', 'viewer');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE billing_status AS ENUM ('active', 'overdue', 'churned');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE contract_status AS ENUM ('trial', 'active', 'past_due', 'ended');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE org_status AS ENUM ('active', 'suspended', 'terminated');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE deployment_environment AS ENUM ('production', 'staging', 'development');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE deployment_health AS ENUM ('healthy', 'degraded', 'down', 'unknown');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE device_status AS ENUM ('active', 'disabled', 'revoked');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE operating_system AS ENUM ('windows', 'macos', 'linux');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE target_type AS ENUM ('customer', 'organization', 'device', 'contract', 'deployment', 'staff');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE action_type AS ENUM (
    'create',
    'update',
    'suspend',
    'reactivate',
    'terminate',
    'disable_device',
    'revoke_device',
    'reactivate_device',
    'revoke_all_devices',
    'mark_contract_ended',
    'mark_overdue',
    'mark_churned',
    'staff_create',
    'staff_deactivate',
    'sign_in'
  );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS staff_users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL UNIQUE,
  name text NOT NULL,
  password_hash text NOT NULL,
  role staff_role NOT NULL DEFAULT 'viewer',
  is_active boolean NOT NULL DEFAULT true,
  must_change_password boolean NOT NULL DEFAULT false,
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS staff_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  staff_user_id uuid NOT NULL REFERENCES staff_users(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS customers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  primary_contact_email text,
  billing_status billing_status NOT NULL DEFAULT 'active',
  notes text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS contracts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id uuid NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  plan text NOT NULL,
  seats integer NOT NULL DEFAULT 0,
  mrr_cents integer NOT NULL DEFAULT 0,
  starts_at timestamptz NOT NULL DEFAULT now(),
  ends_at timestamptz,
  status contract_status NOT NULL DEFAULT 'trial',
  notes text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS organizations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id uuid NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  external_org_id uuid DEFAULT gen_random_uuid(),
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  status org_status NOT NULL DEFAULT 'active',
  status_reason text,
  status_changed_by uuid REFERENCES staff_users(id) ON DELETE SET NULL,
  status_changed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deployments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  environment deployment_environment NOT NULL DEFAULT 'production',
  region text NOT NULL,
  gateway_url text,
  agent_version text,
  last_heartbeat_at timestamptz,
  health deployment_health NOT NULL DEFAULT 'unknown',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS devices (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  external_device_id uuid DEFAULT gen_random_uuid(),
  hostname text NOT NULL,
  os operating_system NOT NULL DEFAULT 'windows',
  agent_version text,
  status device_status NOT NULL DEFAULT 'active',
  status_reason text,
  disabled_at timestamptz,
  revoked_at timestamptz,
  last_seen_at timestamptz,
  enrolled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS control_actions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_staff_id uuid REFERENCES staff_users(id) ON DELETE SET NULL,
  actor_email text NOT NULL,
  target_type target_type NOT NULL,
  target_id uuid NOT NULL,
  action_type action_type NOT NULL,
  reason text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_staff_sessions_staff_user_id ON staff_sessions(staff_user_id);
CREATE INDEX IF NOT EXISTS idx_staff_sessions_expires_at ON staff_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_contracts_customer_id ON contracts(customer_id);
CREATE INDEX IF NOT EXISTS idx_organizations_customer_id ON organizations(customer_id);
CREATE INDEX IF NOT EXISTS idx_organizations_external_org_id ON organizations(external_org_id);
CREATE INDEX IF NOT EXISTS idx_deployments_organization_id ON deployments(organization_id);
CREATE INDEX IF NOT EXISTS idx_devices_organization_id ON devices(organization_id);
CREATE INDEX IF NOT EXISTS idx_devices_external_device_id ON devices(external_device_id);
CREATE INDEX IF NOT EXISTS idx_control_actions_target ON control_actions(target_type, target_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_control_actions_actor ON control_actions(actor_staff_id, created_at DESC);

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_staff_users_updated_at ON staff_users;
CREATE TRIGGER trg_staff_users_updated_at BEFORE UPDATE ON staff_users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_customers_updated_at ON customers;
CREATE TRIGGER trg_customers_updated_at BEFORE UPDATE ON customers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_contracts_updated_at ON contracts;
CREATE TRIGGER trg_contracts_updated_at BEFORE UPDATE ON contracts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_organizations_updated_at ON organizations;
CREATE TRIGGER trg_organizations_updated_at BEFORE UPDATE ON organizations FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_deployments_updated_at ON deployments;
CREATE TRIGGER trg_deployments_updated_at BEFORE UPDATE ON deployments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP TRIGGER IF EXISTS trg_devices_updated_at ON devices;
CREATE TRIGGER trg_devices_updated_at BEFORE UPDATE ON devices FOR EACH ROW EXECUTE FUNCTION set_updated_at();

