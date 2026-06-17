CREATE TABLE IF NOT EXISTS telemetry_events (
    id              BIGSERIAL PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id       TEXT NOT NULL,
    org_id          TEXT NOT NULL,

    request_method  TEXT NOT NULL,
    request_host    TEXT NOT NULL,
    request_path    TEXT,
    request_port    INTEGER,

    response_status INTEGER,
    latency_ms      INTEGER NOT NULL,
    bytes_sent      BIGINT NOT NULL DEFAULT 0,
    bytes_received  BIGINT NOT NULL DEFAULT 0,

    policy_decision TEXT NOT NULL CHECK (policy_decision IN ('allow', 'block', 'log_only')),
    matched_rule_id UUID,
    policy_version  TEXT,

    agent_version   TEXT,
    protocol_version TEXT,
    source_app      TEXT,
    os              TEXT
);

CREATE INDEX IF NOT EXISTS idx_telemetry_org_time
    ON telemetry_events (org_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_telemetry_device_time
    ON telemetry_events (device_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_telemetry_decision
    ON telemetry_events (policy_decision, timestamp DESC)
    WHERE policy_decision != 'allow';

CREATE INDEX IF NOT EXISTS idx_telemetry_host
    ON telemetry_events (request_host, timestamp DESC);
