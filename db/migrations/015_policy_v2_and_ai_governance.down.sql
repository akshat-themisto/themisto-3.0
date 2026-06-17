DROP INDEX IF EXISTS idx_dlp_events_policy_rule;

ALTER TABLE dlp_events
    DROP COLUMN IF EXISTS reason_detail,
    DROP COLUMN IF EXISTS reason_code,
    DROP COLUMN IF EXISTS policy_rule_id;

DROP INDEX IF EXISTS idx_ai_vendor_governance_org;
DROP TABLE IF EXISTS ai_vendor_governance;

DROP INDEX IF EXISTS idx_policy_rules_org_enabled_priority;

ALTER TABLE policy_rules
    DROP COLUMN IF EXISTS block_reason,
    DROP COLUMN IF EXISTS conditions,
    DROP COLUMN IF EXISTS action,
    DROP COLUMN IF EXISTS name;