-- Themisto AI Ledger: endpoint-grounded AI usage, spend, licenses, identity,
-- reconciliation and findings. Provider-specific concepts are intentionally
-- absent from this schema.

CREATE TABLE IF NOT EXISTS ai_ledger_connectors (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    adapter_key             TEXT NOT NULL,
    display_name            TEXT NOT NULL,
    encrypted_credentials   BYTEA NOT NULL,
    config                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    checkpoint              JSONB NOT NULL DEFAULT '{}'::jsonb,
    status                  TEXT NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'syncing', 'healthy', 'degraded', 'revoked', 'error')),
    consecutive_failures    INTEGER NOT NULL DEFAULT 0,
    next_retry_at           TIMESTAMPTZ,
    last_attempt_at         TIMESTAMPTZ,
    last_success_at         TIMESTAMPTZ,
    freshness_at            TIMESTAMPTZ,
    last_error              TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, id)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_connectors_org ON ai_ledger_connectors (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ai_ledger_directory_users (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    identity_source_key     TEXT NOT NULL,
    external_user_id        TEXT NOT NULL,
    normalized_email        TEXT,
    display_name            TEXT,
    status                  TEXT NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'suspended', 'deleted')),
    origin_kind             TEXT NOT NULL CHECK (origin_kind IN ('connector', 'import')),
    source_evidence_level   TEXT NOT NULL CHECK (source_evidence_level IN ('authoritative', 'estimated')),
    reconciliation_status   TEXT NOT NULL DEFAULT 'not_applicable'
                            CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    evidence_level          TEXT NOT NULL CHECK (evidence_level IN ('authoritative', 'reconciled', 'estimated')),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL,
    reporting_period_start  TIMESTAMPTZ,
    reporting_period_end    TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, identity_source_key, external_user_id),
    UNIQUE (org_id, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_directory_email
    ON ai_ledger_directory_users (org_id, normalized_email)
    WHERE normalized_email IS NOT NULL;

CREATE TABLE IF NOT EXISTS ai_ledger_device_user_assignments (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    device_id               UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    directory_user_id       UUID NOT NULL REFERENCES ai_ledger_directory_users(id) ON DELETE CASCADE,
    assigned_from           TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_until          TIMESTAMPTZ,
    assignment_source       TEXT NOT NULL CHECK (assignment_source IN ('mdm', 'admin')),
    assigned_by             UUID REFERENCES admin_users(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (assigned_until IS NULL OR assigned_until > assigned_from),
    UNIQUE (org_id, device_id, directory_user_id, assigned_from)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_device_assignment_active
    ON ai_ledger_device_user_assignments (org_id, device_id, assigned_from, assigned_until);

CREATE TABLE IF NOT EXISTS ai_ledger_products (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    vendor_key              TEXT NOT NULL,
    product_key             TEXT NOT NULL,
    display_name            TEXT NOT NULL,
    functional_category     TEXT NOT NULL DEFAULT 'unknown',
    approved                BOOLEAN NOT NULL DEFAULT false,
    has_contract            BOOLEAN NOT NULL DEFAULT false,
    has_billing_source      BOOLEAN NOT NULL DEFAULT false,
    origin_kind             TEXT NOT NULL CHECK (origin_kind IN ('endpoint', 'connector', 'import')),
    source_evidence_level   TEXT NOT NULL CHECK (source_evidence_level IN ('observed', 'authoritative', 'estimated')),
    reconciliation_status   TEXT NOT NULL CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    evidence_level          TEXT NOT NULL CHECK (evidence_level IN ('observed', 'authoritative', 'reconciled', 'estimated')),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL,
    reporting_period_start  TIMESTAMPTZ,
    reporting_period_end    TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, vendor_key, product_key, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_products_org_product
    ON ai_ledger_products (org_id, vendor_key, product_key);

CREATE TABLE IF NOT EXISTS ai_ledger_external_identities (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    identity_source_key     TEXT NOT NULL,
    identity_kind           TEXT NOT NULL CHECK (identity_kind IN ('account', 'email', 'project', 'directory_user')),
    external_id             TEXT NOT NULL,
    normalized_email        TEXT,
    directory_user_id       UUID REFERENCES ai_ledger_directory_users(id) ON DELETE SET NULL,
    project_key             TEXT,
    reconciliation_status   TEXT NOT NULL DEFAULT 'unmatched'
                            CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    origin_kind             TEXT NOT NULL CHECK (origin_kind IN ('connector', 'import')),
    source_evidence_level   TEXT NOT NULL CHECK (source_evidence_level IN ('authoritative', 'estimated')),
    evidence_level          TEXT NOT NULL CHECK (evidence_level IN ('authoritative', 'reconciled', 'estimated')),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL,
    reporting_period_start  TIMESTAMPTZ,
    reporting_period_end    TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, identity_source_key, identity_kind, external_id),
    UNIQUE (org_id, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_external_identity_email
    ON ai_ledger_external_identities (org_id, normalized_email)
    WHERE normalized_email IS NOT NULL;

CREATE TABLE IF NOT EXISTS ai_ledger_licenses (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    vendor_key              TEXT NOT NULL,
    product_key             TEXT NOT NULL,
    license_key             TEXT NOT NULL,
    external_identity_id    UUID REFERENCES ai_ledger_external_identities(id) ON DELETE SET NULL,
    directory_user_id       UUID REFERENCES ai_ledger_directory_users(id) ON DELETE SET NULL,
    assigned_external_id    TEXT,
    license_status          TEXT NOT NULL DEFAULT 'assigned'
                            CHECK (license_status IN ('assigned', 'unassigned', 'suspended', 'expired')),
    paid                    BOOLEAN NOT NULL DEFAULT true,
    unit_amount             NUMERIC(30,9),
    currency                TEXT,
    billing_interval        TEXT,
    origin_kind             TEXT NOT NULL CHECK (origin_kind IN ('connector', 'import')),
    source_evidence_level   TEXT NOT NULL CHECK (source_evidence_level IN ('authoritative', 'estimated')),
    reconciliation_status   TEXT NOT NULL CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    evidence_level          TEXT NOT NULL CHECK (evidence_level IN ('authoritative', 'reconciled', 'estimated')),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL,
    reporting_period_start  TIMESTAMPTZ,
    reporting_period_end    TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_licenses_org_product
    ON ai_ledger_licenses (org_id, vendor_key, product_key);

CREATE TABLE IF NOT EXISTS ai_ledger_metrics (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    vendor_key              TEXT NOT NULL,
    product_key             TEXT NOT NULL,
    metric_key              TEXT NOT NULL,
    metric_value            NUMERIC(38,9) NOT NULL,
    unit                    TEXT NOT NULL,
    is_activity             BOOLEAN NOT NULL DEFAULT false,
    external_identity_id    UUID REFERENCES ai_ledger_external_identities(id) ON DELETE SET NULL,
    project_key             TEXT,
    model_identifier        TEXT,
    origin_kind             TEXT NOT NULL CHECK (origin_kind IN ('connector', 'import')),
    source_evidence_level   TEXT NOT NULL CHECK (source_evidence_level IN ('authoritative', 'estimated')),
    reconciliation_status   TEXT NOT NULL CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    evidence_level          TEXT NOT NULL CHECK (evidence_level IN ('authoritative', 'reconciled', 'estimated')),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL,
    reporting_period_start  TIMESTAMPTZ NOT NULL,
    reporting_period_end    TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (reporting_period_end > reporting_period_start),
    UNIQUE (org_id, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_metrics_org_period
    ON ai_ledger_metrics (org_id, reporting_period_start, reporting_period_end);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_metric_activity
    ON ai_ledger_metrics (org_id, vendor_key, product_key, reporting_period_end)
    WHERE is_activity = true;

CREATE TABLE IF NOT EXISTS ai_ledger_costs (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    vendor_key              TEXT NOT NULL,
    product_key             TEXT NOT NULL,
    cost_key                TEXT NOT NULL,
    amount                  NUMERIC(30,9) NOT NULL,
    currency                TEXT NOT NULL,
    cost_kind               TEXT NOT NULL DEFAULT 'charge'
                            CHECK (cost_kind IN ('charge', 'credit', 'adjustment', 'tax')),
    external_identity_id    UUID REFERENCES ai_ledger_external_identities(id) ON DELETE SET NULL,
    project_key             TEXT,
    team_key                TEXT,
    origin_kind             TEXT NOT NULL CHECK (origin_kind IN ('connector', 'import')),
    source_evidence_level   TEXT NOT NULL CHECK (source_evidence_level IN ('authoritative', 'estimated')),
    reconciliation_status   TEXT NOT NULL CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    evidence_level          TEXT NOT NULL CHECK (evidence_level IN ('authoritative', 'reconciled', 'estimated')),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL,
    reporting_period_start  TIMESTAMPTZ NOT NULL,
    reporting_period_end    TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (reporting_period_end > reporting_period_start),
    UNIQUE (org_id, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_costs_org_period
    ON ai_ledger_costs (org_id, currency, reporting_period_start, reporting_period_end);

CREATE TABLE IF NOT EXISTS ai_ledger_activity_events (
    id                      BIGSERIAL PRIMARY KEY,
    observed_at             TIMESTAMPTZ NOT NULL,
    device_id               TEXT NOT NULL,
    org_id                  TEXT NOT NULL,
    directory_user_id       UUID REFERENCES ai_ledger_directory_users(id) ON DELETE SET NULL,
    vendor_key              TEXT NOT NULL,
    product_key             TEXT NOT NULL,
    surface                 TEXT NOT NULL,
    activity_kind           TEXT NOT NULL,
    source_application      TEXT,
    model_identifier        TEXT,
    project_identifier      TEXT,
    opaque_session_hash     TEXT,
    activity_count          BIGINT NOT NULL DEFAULT 1 CHECK (activity_count > 0),
    origin_kind             TEXT NOT NULL DEFAULT 'endpoint' CHECK (origin_kind = 'endpoint'),
    source_evidence_level   TEXT NOT NULL DEFAULT 'observed' CHECK (source_evidence_level = 'observed'),
    reconciliation_status   TEXT NOT NULL DEFAULT 'not_applicable'
                            CHECK (reconciliation_status IN ('matched', 'unmatched', 'not_applicable')),
    evidence_level          TEXT NOT NULL DEFAULT 'observed' CHECK (evidence_level = 'observed'),
    source_identifier       TEXT NOT NULL,
    source_key              TEXT NOT NULL,
    external_reference      TEXT,
    freshness_at            TIMESTAMPTZ NOT NULL,
    scope                   TEXT NOT NULL DEFAULT 'device',
    reporting_period_start  TIMESTAMPTZ,
    reporting_period_end    TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, source_identifier, source_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_activity_org_time
    ON ai_ledger_activity_events (org_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_activity_product
    ON ai_ledger_activity_events (org_id, vendor_key, product_key, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_activity_device
    ON ai_ledger_activity_events (org_id, device_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS ai_ledger_findings (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    finding_key             TEXT NOT NULL,
    finding_type            TEXT NOT NULL CHECK (finding_type IN (
                                'inactive_paid_seat', 'orphaned_seat', 'shadow_ai',
                                'unpriced_ai', 'overlapping_seats', 'spend_spike',
                                'unattributed_spend', 'connector_activity_coverage_gap')),
    status                  TEXT NOT NULL DEFAULT 'open'
                            CHECK (status IN ('open', 'reviewed', 'dismissed', 'resolved')),
    severity                TEXT NOT NULL DEFAULT 'review'
                            CHECK (severity IN ('info', 'review', 'important')),
    title                   TEXT NOT NULL,
    summary                 TEXT NOT NULL,
    recommendation          TEXT NOT NULL,
    evidence                JSONB NOT NULL DEFAULT '[]'::jsonb,
    vendor_key              TEXT,
    product_key             TEXT,
    directory_user_id       UUID REFERENCES ai_ledger_directory_users(id) ON DELETE SET NULL,
    currency                TEXT,
    amount                  NUMERIC(30,9),
    first_observed_at       TIMESTAMPTZ NOT NULL,
    last_observed_at        TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, finding_key)
);
CREATE INDEX IF NOT EXISTS idx_ai_ledger_findings_org_status
    ON ai_ledger_findings (org_id, status, last_observed_at DESC);

CREATE TABLE IF NOT EXISTS ai_ledger_imports (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    import_format           TEXT NOT NULL,
    schema_version          TEXT NOT NULL,
    content_hash            TEXT NOT NULL,
    source_identifier       TEXT NOT NULL,
    record_count            INTEGER NOT NULL DEFAULT 0,
    status                  TEXT NOT NULL CHECK (status IN ('validated', 'committed', 'rejected')),
    validation_errors       JSONB NOT NULL DEFAULT '[]'::jsonb,
    imported_at             TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, content_hash)
);

-- Backfill legacy endpoint observations without copying request paths, bodies,
-- local usernames, or other content-bearing telemetry. One legacy request row
-- becomes one canonical observed row, preserving aggregate event counts.
INSERT INTO ai_ledger_activity_events (
    observed_at, device_id, org_id, vendor_key, product_key, surface,
    activity_kind, source_application, activity_count, origin_kind,
    source_evidence_level, reconciliation_status, evidence_level,
    source_identifier, source_key, freshness_at, scope
)
SELECT
    t.timestamp,
    t.device_id,
    t.org_id,
    lower(t.ai_vendor),
    'legacy_' || regexp_replace(lower(t.ai_vendor), '[^a-z0-9_]+', '_', 'g'),
    COALESCE(NULLIF(lower(t.capture_surface), ''), 'network_proxy'),
    'request',
    CASE
        WHEN t.source_app IS NULL OR t.source_app ~ '[/\\]' THEN NULL
        ELSE left(t.source_app, 128)
    END,
    1,
    'endpoint',
    'observed',
    'not_applicable',
    'observed',
    t.device_id,
    'legacy-telemetry-' || t.id::text,
    t.timestamp,
    'device'
FROM telemetry_events t
WHERE t.ai_vendor IS NOT NULL
  AND trim(t.ai_vendor) <> ''
ON CONFLICT (org_id, source_identifier, source_key) DO NOTHING;

INSERT INTO ai_ledger_products (
    org_id, vendor_key, product_key, display_name, functional_category,
    approved, origin_kind, source_evidence_level, reconciliation_status,
    evidence_level, source_identifier, source_key, freshness_at, scope
)
SELECT
    o.id,
    e.vendor_key,
    e.product_key,
    initcap(replace(e.vendor_key, '_', ' ')) || ' (legacy endpoint)',
    'unknown',
    false,
    'endpoint',
    'observed',
    'not_applicable',
    'observed',
    'legacy-backfill',
    e.vendor_key || '/' || e.product_key,
    max(e.observed_at),
    'organization'
FROM organizations o
JOIN ai_ledger_activity_events e ON e.org_id = o.id::text
GROUP BY o.id, e.vendor_key, e.product_key
ON CONFLICT (org_id, vendor_key, product_key, source_identifier, source_key) DO NOTHING;

-- Older agent versions reported the local OS login name in status telemetry.
-- It is neither a directory identity nor permitted central telemetry.
UPDATE agent_status_events
SET data = data - 'agent_user'
WHERE data ? 'agent_user';

-- Enforce the new no-content central-telemetry boundary for legacy rows.
UPDATE telemetry_events SET request_path = NULL WHERE request_path IS NOT NULL;
UPDATE dlp_events
SET request_path = NULL,
    matched_patterns = '{}'::text[],
    matched_fields = '{}'::text[],
    reason_detail = NULL,
    semantic_reason = NULL,
    request_body_encrypted = NULL,
    request_body_nonce = NULL,
    request_body_truncated = false
WHERE request_path IS NOT NULL
   OR cardinality(matched_patterns) > 0
   OR cardinality(matched_fields) > 0
   OR reason_detail IS NOT NULL
   OR semantic_reason IS NOT NULL
   OR request_body_encrypted IS NOT NULL;
