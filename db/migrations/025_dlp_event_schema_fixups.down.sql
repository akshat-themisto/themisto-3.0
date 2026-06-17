DROP INDEX IF EXISTS idx_alert_reads_user_read_at;
DROP TABLE IF EXISTS admin_user_alert_reads;

DROP INDEX IF EXISTS idx_dlp_events_body_retention;
DROP INDEX IF EXISTS idx_dlp_events_org_severity_time;
DROP INDEX IF EXISTS idx_dlp_events_direction_time;
DROP INDEX IF EXISTS idx_dlp_events_protocol_time;
DROP INDEX IF EXISTS idx_dlp_events_policy_rule;

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_severity_check,
    DROP CONSTRAINT IF EXISTS dlp_events_direction_check,
    DROP CONSTRAINT IF EXISTS dlp_events_inspection_quality_check,
    DROP CONSTRAINT IF EXISTS dlp_events_protocol_check,
    DROP CONSTRAINT IF EXISTS dlp_events_action_taken_check;

ALTER TABLE dlp_events
    DROP COLUMN IF EXISTS matched_fields,
    DROP COLUMN IF EXISTS file_count,
    DROP COLUMN IF EXISTS content_type,
    DROP COLUMN IF EXISTS classification_reason,
    DROP COLUMN IF EXISTS severity,
    DROP COLUMN IF EXISTS direction,
    DROP COLUMN IF EXISTS inspection_skip_reason,
    DROP COLUMN IF EXISTS inspection_quality,
    DROP COLUMN IF EXISTS intercepted_https,
    DROP COLUMN IF EXISTS protocol,
    DROP COLUMN IF EXISTS reason_detail,
    DROP COLUMN IF EXISTS reason_code,
    DROP COLUMN IF EXISTS policy_rule_id,
    DROP COLUMN IF EXISTS request_body_truncated,
    DROP COLUMN IF EXISTS request_body_nonce,
    DROP COLUMN IF EXISTS request_body_encrypted;
