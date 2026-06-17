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
    match_types      TEXT[]  NOT NULL DEFAULT '{}',
    matched_patterns TEXT[]  NOT NULL DEFAULT '{}',
    match_count      INTEGER NOT NULL DEFAULT 0,
    action_taken     TEXT NOT NULL DEFAULT 'alert' CHECK (action_taken IN ('alert', 'block', 'redact'))
);

CREATE INDEX IF NOT EXISTS idx_dlp_events_org_time
    ON dlp_events (org_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_dlp_events_device
    ON dlp_events (device_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_dlp_events_ai_vendor
    ON dlp_events (ai_vendor, timestamp DESC)
    WHERE ai_vendor IS NOT NULL;
