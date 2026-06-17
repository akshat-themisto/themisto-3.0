-- Add AI service classification columns to telemetry_events.
ALTER TABLE telemetry_events
    ADD COLUMN IF NOT EXISTS service_category TEXT,
    ADD COLUMN IF NOT EXISTS ai_vendor        TEXT;

CREATE INDEX IF NOT EXISTS idx_telemetry_ai_vendor
    ON telemetry_events (ai_vendor, timestamp DESC)
    WHERE ai_vendor IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_telemetry_service_category
    ON telemetry_events (service_category, timestamp DESC)
    WHERE service_category IS NOT NULL;
