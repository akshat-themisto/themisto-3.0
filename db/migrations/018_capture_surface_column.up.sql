ALTER TABLE telemetry_events ADD COLUMN IF NOT EXISTS capture_surface TEXT;
CREATE INDEX IF NOT EXISTS idx_telemetry_capture_surface
    ON telemetry_events (capture_surface, timestamp DESC)
    WHERE capture_surface IS NOT NULL;
