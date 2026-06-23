DROP INDEX IF EXISTS idx_dlp_events_semantic_source_time;

ALTER TABLE dlp_events
    DROP COLUMN IF EXISTS semantic_reason,
    DROP COLUMN IF EXISTS semantic_ambiguous,
    DROP COLUMN IF EXISTS semantic_confidence,
    DROP COLUMN IF EXISTS semantic_category,
    DROP COLUMN IF EXISTS semantic_source;
