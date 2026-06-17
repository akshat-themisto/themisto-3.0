DROP INDEX IF EXISTS idx_telemetry_ai_vendor;
DROP INDEX IF EXISTS idx_telemetry_service_category;
ALTER TABLE telemetry_events
    DROP COLUMN IF EXISTS service_category,
    DROP COLUMN IF EXISTS ai_vendor;
