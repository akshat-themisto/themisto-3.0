ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'http',
    ADD COLUMN IF NOT EXISTS intercepted_https BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS inspection_quality TEXT NOT NULL DEFAULT 'full',
    ADD COLUMN IF NOT EXISTS inspection_skip_reason TEXT,
    ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT 'outbound';

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_protocol_check;
ALTER TABLE dlp_events
    ADD CONSTRAINT dlp_events_protocol_check
    CHECK (protocol IN ('http', 'websocket', 'grpc'));

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_inspection_quality_check;
ALTER TABLE dlp_events
    ADD CONSTRAINT dlp_events_inspection_quality_check
    CHECK (inspection_quality IN ('full', 'partial', 'skipped'));

ALTER TABLE dlp_events
    DROP CONSTRAINT IF EXISTS dlp_events_direction_check;
ALTER TABLE dlp_events
    ADD CONSTRAINT dlp_events_direction_check
    CHECK (direction IN ('outbound', 'inbound'));

CREATE INDEX IF NOT EXISTS idx_dlp_events_protocol_time
    ON dlp_events (protocol, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_dlp_events_direction_time
    ON dlp_events (direction, timestamp DESC);
