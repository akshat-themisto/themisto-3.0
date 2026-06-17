DROP INDEX IF EXISTS idx_dlp_events_direction_time;
DROP INDEX IF EXISTS idx_dlp_events_protocol_time;

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_direction_check,
    DROP CONSTRAINT IF EXISTS dlp_events_inspection_quality_check,
    DROP CONSTRAINT IF EXISTS dlp_events_protocol_check;

ALTER TABLE dlp_events
    DROP COLUMN IF EXISTS direction,
    DROP COLUMN IF EXISTS inspection_skip_reason,
    DROP COLUMN IF EXISTS inspection_quality,
    DROP COLUMN IF EXISTS intercepted_https,
    DROP COLUMN IF EXISTS protocol;
