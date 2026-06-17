ALTER TABLE policy_rules
    ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS action TEXT NOT NULL DEFAULT 'alert' CHECK (action IN ('allow', 'alert', 'block')),
    ADD COLUMN IF NOT EXISTS conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS block_reason TEXT;

UPDATE policy_rules
SET action = CASE decision
    WHEN 'allow' THEN 'allow'
    WHEN 'block' THEN 'block'
    ELSE 'alert'
END;

UPDATE policy_rules
SET name = 'Policy rule ' || priority::text
WHERE COALESCE(NULLIF(name, ''), '') = '';

UPDATE policy_rules
SET conditions =
    (CASE
        WHEN match_host IS NOT NULL AND match_host <> ''
            THEN jsonb_build_array(
                jsonb_build_object('field', 'host', 'operator', 'contains', 'value', match_host)
            )
        ELSE '[]'::jsonb
    END) ||
    (CASE
        WHEN match_path IS NOT NULL AND match_path <> ''
            THEN jsonb_build_array(
                jsonb_build_object('field', 'path', 'operator', 'contains', 'value', match_path)
            )
        ELSE '[]'::jsonb
    END) ||
    (CASE
        WHEN match_method IS NOT NULL AND match_method <> ''
            THEN jsonb_build_array(
                jsonb_build_object('field', 'method', 'operator', 'eq', 'value', match_method)
            )
        ELSE '[]'::jsonb
    END)
WHERE conditions = '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_policy_rules_org_enabled_priority
    ON policy_rules (org_id, enabled, priority);

CREATE TABLE IF NOT EXISTS ai_vendor_governance (
    id          BIGSERIAL PRIMARY KEY,
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    ai_vendor   TEXT NOT NULL,
    sanctioned  BOOLEAN NOT NULL DEFAULT FALSE,
    risk_tier   TEXT NOT NULL DEFAULT 'medium' CHECK (risk_tier IN ('low', 'medium', 'high', 'critical')),
    notes       TEXT,
    updated_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, ai_vendor)
);

CREATE INDEX IF NOT EXISTS idx_ai_vendor_governance_org
    ON ai_vendor_governance (org_id, ai_vendor);

INSERT INTO ai_vendor_governance (org_id, ai_vendor, sanctioned, risk_tier)
SELECT DISTINCT o.id, t.ai_vendor, FALSE, 'medium'
FROM telemetry_events t
JOIN organizations o ON o.id::text = t.org_id
WHERE t.ai_vendor IS NOT NULL AND t.ai_vendor <> ''
ON CONFLICT (org_id, ai_vendor) DO NOTHING;

ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS policy_rule_id TEXT,
    ADD COLUMN IF NOT EXISTS reason_code TEXT,
    ADD COLUMN IF NOT EXISTS reason_detail TEXT;

CREATE INDEX IF NOT EXISTS idx_dlp_events_policy_rule
    ON dlp_events (policy_rule_id)
    WHERE policy_rule_id IS NOT NULL;