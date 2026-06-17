DROP INDEX IF EXISTS idx_dlp_events_body_retention;
DROP INDEX IF EXISTS idx_alert_reads_user_read_at;
DROP TABLE IF EXISTS admin_user_alert_reads;

DROP INDEX IF EXISTS idx_dlp_events_org_severity_time;

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_severity_check;

ALTER TABLE dlp_events
    DROP COLUMN IF EXISTS matched_fields,
    DROP COLUMN IF EXISTS file_count,
    DROP COLUMN IF EXISTS content_type,
    DROP COLUMN IF EXISTS classification_reason,
    DROP COLUMN IF EXISTS severity;

DROP INDEX IF EXISTS idx_ai_intercept_domains_org_enabled;
DROP TABLE IF EXISTS ai_intercept_domains;
