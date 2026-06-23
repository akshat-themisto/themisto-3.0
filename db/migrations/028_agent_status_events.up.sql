CREATE TABLE IF NOT EXISTS agent_status_events (
    id          BIGSERIAL PRIMARY KEY,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id   TEXT NOT NULL,
    org_id      TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    severity    TEXT NOT NULL DEFAULT 'info'
                CHECK (severity IN ('info', 'warning', 'critical')),
    data        JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_agent_status_org_time
    ON agent_status_events (org_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_agent_status_device_time
    ON agent_status_events (device_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_agent_status_incidents
    ON agent_status_events (severity, timestamp DESC)
    WHERE severity != 'info';
