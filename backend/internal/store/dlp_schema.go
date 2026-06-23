package store

import "context"

// EnsureDLPSchema makes old dev databases compatible with the current DLP event
// ingestion and dashboard queries. Real migrations remain the source of truth;
// this is a startup guard for reused local volumes.
func (s *Store) EnsureDLPSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS dlp_events (
    id               BIGSERIAL PRIMARY KEY,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id        TEXT NOT NULL,
    org_id           TEXT NOT NULL,
    request_id       TEXT,
    request_host     TEXT NOT NULL,
    request_path     TEXT,
    request_method   TEXT NOT NULL,
    source_app       TEXT,
    service_category TEXT,
    ai_vendor        TEXT,
    match_types      TEXT[] NOT NULL DEFAULT '{}',
    matched_patterns TEXT[] NOT NULL DEFAULT '{}',
    match_count      INTEGER NOT NULL DEFAULT 0,
    action_taken     TEXT NOT NULL DEFAULT 'alert'
);

ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS request_body_encrypted BYTEA,
    ADD COLUMN IF NOT EXISTS request_body_nonce BYTEA,
    ADD COLUMN IF NOT EXISTS request_body_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS policy_rule_id TEXT,
    ADD COLUMN IF NOT EXISTS reason_code TEXT,
    ADD COLUMN IF NOT EXISTS reason_detail TEXT,
    ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'http',
    ADD COLUMN IF NOT EXISTS intercepted_https BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS inspection_quality TEXT NOT NULL DEFAULT 'full',
    ADD COLUMN IF NOT EXISTS inspection_skip_reason TEXT,
    ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT 'outbound',
    ADD COLUMN IF NOT EXISTS severity TEXT NOT NULL DEFAULT 'low',
    ADD COLUMN IF NOT EXISTS classification_reason TEXT,
    ADD COLUMN IF NOT EXISTS content_type TEXT,
    ADD COLUMN IF NOT EXISTS file_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS matched_fields TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS semantic_source TEXT,
    ADD COLUMN IF NOT EXISTS semantic_category TEXT,
    ADD COLUMN IF NOT EXISTS semantic_confidence DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS semantic_ambiguous BOOLEAN,
    ADD COLUMN IF NOT EXISTS semantic_reason TEXT;

UPDATE dlp_events SET action_taken = 'alert' WHERE action_taken IS NULL OR action_taken = '';
UPDATE dlp_events SET protocol = 'http' WHERE protocol IS NULL OR protocol = '';
UPDATE dlp_events SET inspection_quality = 'full' WHERE inspection_quality IS NULL OR inspection_quality = '';
UPDATE dlp_events SET direction = 'outbound' WHERE direction IS NULL OR direction = '';
UPDATE dlp_events SET severity = 'low' WHERE severity IS NULL OR severity = '';
UPDATE dlp_events SET match_types = '{}' WHERE match_types IS NULL;
UPDATE dlp_events SET matched_patterns = '{}' WHERE matched_patterns IS NULL;
UPDATE dlp_events SET matched_fields = '{}' WHERE matched_fields IS NULL;
UPDATE dlp_events SET match_count = 0 WHERE match_count IS NULL;
UPDATE dlp_events SET file_count = 0 WHERE file_count IS NULL;
UPDATE dlp_events SET request_body_truncated = FALSE WHERE request_body_truncated IS NULL;
UPDATE dlp_events SET intercepted_https = FALSE WHERE intercepted_https IS NULL;

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_action_taken_check,
    DROP CONSTRAINT IF EXISTS dlp_events_protocol_check,
    DROP CONSTRAINT IF EXISTS dlp_events_inspection_quality_check,
    DROP CONSTRAINT IF EXISTS dlp_events_direction_check,
    DROP CONSTRAINT IF EXISTS dlp_events_severity_check;

ALTER TABLE dlp_events
    ADD CONSTRAINT dlp_events_action_taken_check
    CHECK (action_taken IN ('alert', 'block', 'redact')),
    ADD CONSTRAINT dlp_events_protocol_check
    CHECK (protocol IN ('http', 'https', 'connect')),
    ADD CONSTRAINT dlp_events_inspection_quality_check
    CHECK (inspection_quality IN ('full', 'metadata_only', 'skipped')),
    ADD CONSTRAINT dlp_events_direction_check
    CHECK (direction IN ('outbound', 'inbound')),
    ADD CONSTRAINT dlp_events_severity_check
    CHECK (severity IN ('low', 'medium', 'high', 'critical'));

CREATE INDEX IF NOT EXISTS idx_dlp_events_org_time
    ON dlp_events (org_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_device
    ON dlp_events (device_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_ai_vendor
    ON dlp_events (ai_vendor, timestamp DESC)
    WHERE ai_vendor IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dlp_events_policy_rule
    ON dlp_events (policy_rule_id)
    WHERE policy_rule_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dlp_events_protocol_time
    ON dlp_events (protocol, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_direction_time
    ON dlp_events (direction, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_org_severity_time
    ON dlp_events (org_id, severity, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_dlp_events_body_retention
    ON dlp_events (timestamp)
    WHERE request_body_encrypted IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_dlp_events_semantic_source_time
    ON dlp_events (semantic_source, timestamp DESC)
    WHERE semantic_source IS NOT NULL;

CREATE TABLE IF NOT EXISTS admin_user_alert_reads (
    user_id      UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    dlp_event_id BIGINT NOT NULL REFERENCES dlp_events(id) ON DELETE CASCADE,
    read_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, dlp_event_id)
);

CREATE INDEX IF NOT EXISTS idx_alert_reads_user_read_at
    ON admin_user_alert_reads (user_id, read_at DESC);
`)
	return err
}
