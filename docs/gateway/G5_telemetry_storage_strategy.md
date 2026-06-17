# G5 — Telemetry Storage & Retention Strategy

PostgreSQL telemetry storage on Ubuntu VPS for the Themisto gateway.

---

## 1. Table Schema

### 1.1 Primary Telemetry Table

```sql
CREATE TABLE telemetry_events (
    id              BIGSERIAL,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id       UUID NOT NULL,
    org_id          UUID NOT NULL,

    -- Request metadata
    request_method  TEXT NOT NULL,
    request_host    TEXT NOT NULL,
    request_path    TEXT,
    request_port    INTEGER,

    -- Response metadata
    response_status INTEGER,
    latency_ms      INTEGER NOT NULL,
    bytes_sent      BIGINT NOT NULL DEFAULT 0,
    bytes_received  BIGINT NOT NULL DEFAULT 0,

    -- Policy metadata
    policy_decision TEXT NOT NULL CHECK (policy_decision IN ('allow', 'block', 'log_only')),
    matched_rule_id UUID,
    policy_version  TEXT,

    -- Agent metadata
    agent_version   TEXT,
    protocol_version TEXT,
    source_app      TEXT,
    os              TEXT,

    -- Partitioning key
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);
```

### 1.2 Partitioning

Create monthly partitions. Partitioning by time enables efficient retention (drop old partitions) and query performance (partition pruning on time range).

```sql
-- Create partitions for each month
CREATE TABLE telemetry_events_2025_01
    PARTITION OF telemetry_events
    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

CREATE TABLE telemetry_events_2025_02
    PARTITION OF telemetry_events
    FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');

-- ... repeat per month
```

**Automation:** A cron job or PL/pgSQL function creates next month's partition 7 days in advance. Alert if the partition doesn't exist (inserts will fail).

```sql
CREATE OR REPLACE FUNCTION create_next_partition()
RETURNS void AS $$
DECLARE
    next_month DATE := date_trunc('month', now() + interval '1 month');
    end_month DATE := next_month + interval '1 month';
    partition_name TEXT := 'telemetry_events_' || to_char(next_month, 'YYYY_MM');
BEGIN
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF telemetry_events
         FOR VALUES FROM (%L) TO (%L)',
        partition_name, next_month, end_month
    );
END;
$$ LANGUAGE plpgsql;
```

### 1.3 Aggregation Table (Hourly Summaries)

For quick dashboard-less operational queries (top hosts, error rates), maintain pre-aggregated hourly summaries.

```sql
CREATE TABLE telemetry_hourly (
    hour            TIMESTAMPTZ NOT NULL,
    org_id          UUID NOT NULL,
    request_host    TEXT NOT NULL,
    policy_decision TEXT NOT NULL,

    request_count   BIGINT NOT NULL DEFAULT 0,
    error_count     BIGINT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    total_bytes     BIGINT NOT NULL DEFAULT 0,

    PRIMARY KEY (hour, org_id, request_host, policy_decision)
);
```

Populated by a background worker that runs every hour, aggregating the previous hour's `telemetry_events` rows.

---

## 2. Index Strategy

### 2.1 Indexes on Partitioned Table

Indexes are defined on the parent table and automatically created on each partition.

```sql
-- Primary query pattern: by org + time range
CREATE INDEX idx_telemetry_org_time
    ON telemetry_events (org_id, timestamp DESC);

-- Device-specific lookups
CREATE INDEX idx_telemetry_device_time
    ON telemetry_events (device_id, timestamp DESC);

-- Policy decision filtering (blocked requests)
CREATE INDEX idx_telemetry_decision
    ON telemetry_events (policy_decision, timestamp DESC)
    WHERE policy_decision != 'allow';

-- Host-based queries
CREATE INDEX idx_telemetry_host
    ON telemetry_events (request_host, timestamp DESC);
```

### 2.2 Index Design Rationale

| Index | Justification |
|-------|--------------|
| `(org_id, timestamp DESC)` | Primary access pattern: "show me events for org X in the last hour" |
| `(device_id, timestamp DESC)` | Troubleshooting: "what did device Y do recently?" |
| `(policy_decision, timestamp)` partial | Security: "show me all blocked requests" — partial index keeps it small |
| `(request_host, timestamp DESC)` | Operational: "traffic to host Z" |

### 2.3 What Not to Index

- `request_path`: High cardinality, rarely queried alone. Use `LIKE` with leading host filter.
- `agent_version`, `os`, `source_app`: Low-value for indexed lookups. Filter after org/time narrowing.
- `bytes_sent`, `bytes_received`, `latency_ms`: Aggregated, not filtered. No index needed.

---

## 3. Write Optimization

### 3.1 Batch Inserts

The gateway's telemetry buffer (G3) flushes events in batches. The insert uses a single multi-row INSERT:

```sql
INSERT INTO telemetry_events (
    timestamp, device_id, org_id,
    request_method, request_host, request_path, request_port,
    response_status, latency_ms, bytes_sent, bytes_received,
    policy_decision, matched_rule_id, policy_version,
    agent_version, protocol_version, source_app, os
) VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18),
    ($19, $20, ...),
    ...;
```

Batch size: 500 rows per INSERT (tunable). At 500 rows with ~20 columns, each batch is roughly 25-50 KB of SQL — well within PostgreSQL's capacity.

### 3.2 Connection Pooling

- Gateway maintains a dedicated connection pool for telemetry writes.
- Pool size: 5 connections (separate from the main application pool).
- This prevents telemetry write bursts from starving enrollment/policy queries.

### 3.3 Async Write Pipeline

```
Request Handler
      │
      ▼
Telemetry Channel (buffered, 10,000)
      │
      ▼
Flush Worker (goroutine)
  - Accumulate up to 500 events or 1 second
  - Batch INSERT via telemetry DB pool
  - On failure: retry once, then drop batch + increment dropped counter
```

### 3.4 PostgreSQL Tuning for Write Workload

```
# postgresql.conf adjustments for telemetry workload

# WAL
wal_level = replica
wal_buffers = 64MB
max_wal_size = 2GB
min_wal_size = 512MB

# Checkpoints
checkpoint_completion_target = 0.9
checkpoint_timeout = 10min

# Background writer
bgwriter_lru_maxpages = 200
bgwriter_delay = 50ms

# Autovacuum (aggressive for append-heavy table)
autovacuum_naptime = 30s
autovacuum_vacuum_threshold = 10000
autovacuum_analyze_threshold = 5000

# Shared buffers (25% of RAM, max ~8GB)
shared_buffers = 1GB      # for 4GB VPS

# Effective cache
effective_cache_size = 3GB  # for 4GB VPS

# Work mem (per-sort, keep modest)
work_mem = 16MB
maintenance_work_mem = 256MB
```

### 3.5 UNLOGGED Table Option (Not Recommended)

`UNLOGGED` tables skip WAL, giving ~2x write throughput, but data is lost on crash. Since telemetry data has audit value, keep the table logged. Accept the write overhead.

---

## 4. Retention Policy

### 4.1 Tiered Retention

| Data | Retention | Mechanism |
|------|-----------|-----------|
| Raw telemetry events | 90 days | Drop old partitions |
| Hourly aggregations | 1 year | DELETE + VACUUM |
| Audit log | 1 year (minimum) | Archive to cold storage, then DELETE |

### 4.2 Partition Drop (Raw Events)

Monthly cron job drops partitions older than 90 days:

```bash
#!/usr/bin/env bash
# /opt/themisto/scripts/telemetry_retention.sh
set -euo pipefail

RETAIN_MONTHS=3
CUTOFF=$(date -d "-${RETAIN_MONTHS} months" +%Y_%m)

docker exec themisto-postgres-1 psql -U themisto -d themisto -c "
    DO \$\$
    DECLARE
        r RECORD;
    BEGIN
        FOR r IN
            SELECT tablename FROM pg_tables
            WHERE tablename LIKE 'telemetry_events_%'
              AND tablename < 'telemetry_events_${CUTOFF}'
        LOOP
            EXECUTE 'DROP TABLE IF EXISTS ' || quote_ident(r.tablename);
            RAISE NOTICE 'Dropped partition: %', r.tablename;
        END LOOP;
    END \$\$;
"
```

**Why DROP over DELETE:** Dropping a partition is O(1) — it removes files instantly. DELETE + VACUUM on millions of rows is expensive and creates I/O pressure.

### 4.3 Aggregation Cleanup

```sql
DELETE FROM telemetry_hourly WHERE hour < now() - interval '1 year';
VACUUM telemetry_hourly;
```

Run monthly after the partition drop.

---

## 5. Disk Growth Monitoring

### 5.1 Estimated Growth Rate

| Metric | Value |
|--------|-------|
| Average event row size | ~500 bytes (including indexes) |
| Events per device per day | ~5,000 (typical browsing) |
| 100 devices | ~250 MB/day raw |
| 1,000 devices | ~2.5 GB/day raw |
| 90-day retention (100 devices) | ~22 GB |
| 90-day retention (1,000 devices) | ~220 GB |

### 5.2 Monitoring Queries

**Current table sizes:**
```sql
SELECT
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname || '.' || tablename)) AS total_size
FROM pg_tables
WHERE tablename LIKE 'telemetry%'
ORDER BY pg_total_relation_size(schemaname || '.' || tablename) DESC;
```

**Daily growth rate:**
```sql
SELECT
    date_trunc('day', timestamp) AS day,
    count(*) AS event_count,
    pg_size_pretty(count(*) * 500) AS estimated_size
FROM telemetry_events
WHERE timestamp > now() - interval '7 days'
GROUP BY 1
ORDER BY 1;
```

**Projected disk exhaustion:**
```bash
#!/usr/bin/env bash
# /opt/themisto/scripts/disk_growth_check.sh

DISK_TOTAL=$(df /var/lib/docker --output=size | tail -1 | tr -d ' ')
DISK_USED=$(df /var/lib/docker --output=used | tail -1 | tr -d ' ')
DISK_FREE=$((DISK_TOTAL - DISK_USED))

DB_SIZE=$(docker exec themisto-postgres-1 psql -U themisto -tAc \
  "SELECT pg_database_size('themisto');")

DAILY_GROWTH=$(docker exec themisto-postgres-1 psql -U themisto -tAc "
  SELECT coalesce(
    (SELECT count(*) * 500
     FROM telemetry_events
     WHERE timestamp > now() - interval '1 day'),
    0
  );
")

if [ "$DAILY_GROWTH" -gt 0 ]; then
  DAYS_LEFT=$((DISK_FREE * 1024 / DAILY_GROWTH))
  if [ "$DAYS_LEFT" -lt 14 ]; then
    echo "ALERT: Disk full in ~${DAYS_LEFT} days" | \
      mail -s "Themisto Disk Growth Alert" ops@example.com
  fi
fi
```

### 5.3 Alerting Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| Disk usage | 70% | 85% |
| DB size / disk ratio | 60% | 75% |
| Days until full (projected) | <30 days | <14 days |
| Partition count (check cron ran) | >4 months of partitions | >6 months |
| telemetry_events row count (current month) | Informational | >100M rows/month |

### 5.4 Automated Response

When disk usage exceeds 85%:
1. Alert fires (email/webhook).
2. If retention partitions exist beyond policy (>90 days), auto-drop oldest.
3. If within policy, alert operator for manual intervention (add disk, reduce retention, archive).
4. Gateway telemetry buffer starts dropping events (lossy mode) to prevent DB write failures from cascading.

---

## 6. Backup Policy

### 6.1 What to Back Up

| Component | Included in Backup | Method |
|-----------|-------------------|--------|
| Raw telemetry events | Last 7 days only (recent partitions) | pg_dump with partition filter |
| Hourly aggregations | Full table | pg_dump |
| Telemetry schema (DDL) | Yes | pg_dump --schema-only |
| Full database | Yes (includes enrollment, certs, etc.) | pg_dump (see G1, Section 7) |

### 6.2 Backup Strategy

The telemetry data is **reproducible** in the sense that it's derived from real-time traffic — once lost, it cannot be regenerated, but its loss is non-fatal (enrollment data and CA keys are far more critical).

**Tiered backup:**

| Tier | Content | Frequency | Retention |
|------|---------|-----------|-----------|
| Full DB dump | Everything | Daily 2:00 AM | 30 days |
| Schema-only | DDL for disaster recovery | Weekly | 90 days |
| WAL archiving (future) | Point-in-time recovery | Continuous | 7 days |

### 6.3 Restore Priority

In a disaster recovery scenario:
1. **First:** Restore enrollment data, certificates, organizations (critical for service).
2. **Second:** Restore policy rules.
3. **Third:** Restore recent telemetry (nice to have).
4. **Last:** Restore hourly aggregations (can be re-derived if raw data exists).

Telemetry loss is acceptable if it means faster service restoration.

### 6.4 Backup Verification

Monthly test:
1. Restore latest backup to a temporary PostgreSQL instance.
2. Verify partition structure exists.
3. Run sample queries against telemetry data.
4. Confirm row counts are within expected range.
5. Drop temporary instance.
