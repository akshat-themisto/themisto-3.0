ALTER TABLE dlp_events
    ADD COLUMN IF NOT EXISTS semantic_source TEXT,
    ADD COLUMN IF NOT EXISTS semantic_category TEXT,
    ADD COLUMN IF NOT EXISTS semantic_confidence DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS semantic_ambiguous BOOLEAN,
    ADD COLUMN IF NOT EXISTS semantic_reason TEXT;

CREATE INDEX IF NOT EXISTS idx_dlp_events_semantic_source_time
    ON dlp_events (semantic_source, timestamp DESC)
    WHERE semantic_source IS NOT NULL;
