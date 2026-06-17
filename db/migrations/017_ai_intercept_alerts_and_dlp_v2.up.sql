CREATE TABLE IF NOT EXISTS ai_intercept_domains (
    id         BIGSERIAL PRIMARY KEY,
    org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain     TEXT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    source     TEXT NOT NULL DEFAULT 'managed' CHECK (source IN ('managed', 'admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, domain)
);

CREATE INDEX IF NOT EXISTS idx_ai_intercept_domains_org_enabled
    ON ai_intercept_domains (org_id, enabled, domain);

ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS severity TEXT NOT NULL DEFAULT 'low',
    ADD COLUMN IF NOT EXISTS classification_reason TEXT,
    ADD COLUMN IF NOT EXISTS content_type TEXT,
    ADD COLUMN IF NOT EXISTS file_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS matched_fields TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_severity_check;
ALTER TABLE dlp_events
    ADD CONSTRAINT dlp_events_severity_check
    CHECK (severity IN ('low', 'medium', 'high', 'critical'));

CREATE INDEX IF NOT EXISTS idx_dlp_events_org_severity_time
    ON dlp_events (org_id, severity, timestamp DESC);

CREATE TABLE IF NOT EXISTS admin_user_alert_reads (
    user_id      UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    dlp_event_id BIGINT NOT NULL REFERENCES dlp_events(id) ON DELETE CASCADE,
    read_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, dlp_event_id)
);

CREATE INDEX IF NOT EXISTS idx_alert_reads_user_read_at
    ON admin_user_alert_reads (user_id, read_at DESC);

CREATE INDEX IF NOT EXISTS idx_dlp_events_body_retention
    ON dlp_events (timestamp)
    WHERE request_body_encrypted IS NOT NULL;
