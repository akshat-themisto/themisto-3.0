DROP INDEX IF EXISTS idx_telemetry_capture_surface;
ALTER TABLE telemetry_events DROP COLUMN IF EXISTS capture_surface;
