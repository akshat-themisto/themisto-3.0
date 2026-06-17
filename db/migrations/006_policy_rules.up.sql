CREATE TABLE IF NOT EXISTS policy_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    priority        INTEGER NOT NULL,
    match_host      TEXT,
    match_path      TEXT,
    match_method    TEXT,
    decision        TEXT NOT NULL CHECK (decision IN ('allow', 'block', 'log_only')),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (org_id, priority)
);

CREATE INDEX IF NOT EXISTS idx_policy_rules_org_id ON policy_rules(org_id)
    WHERE enabled = true;
