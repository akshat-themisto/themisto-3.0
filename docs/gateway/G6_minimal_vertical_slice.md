# G6 — Minimal Ubuntu Vertical Slice Definition

Absolute minimal cloud implementation for the end-to-end flow:
**Agent → mTLS → Gateway → Upstream → Log → Store**

No dashboard, no DLP, no scaling, no multi-region.

---

## 1. Component Classification

### 1.1 Real (Production-Grade) Components

These must work correctly and securely from day one. No shortcuts.

| Component | Why Real |
|-----------|----------|
| CA key generation (ECDSA P-384) | Security foundation — cannot be faked |
| Certificate signing (CSR → cert) | Core trust chain — must be correct |
| mTLS handshake (gateway TLS listener) | Security boundary — cannot be stubbed |
| Agent client cert authentication | Identity assertion — must be real |
| TLS 1.3 enforcement | Compliance requirement |
| Certificate revocation check | Security — must reject revoked certs |
| Audit logging (cert.issued, cert.revoked) | Accountability — must be real from start |

### 1.2 Hardcoded Components

Simplified with static values for the minimal slice. Will be replaced with dynamic implementations later.

| Component | Hardcoded Value | Future State |
|-----------|----------------|--------------|
| Organization | Single org, hardcoded UUID + name | Multi-org via API |
| Policy rules | Allow-all (no blocking rules) | Per-org policy management API |
| Admin authentication | Single API key in env var | OAuth / RBAC |
| Gateway config | Static YAML, no hot-reload | Dynamic config + watch |
| Protocol version | `2025.01` hardcoded | Version negotiation |
| Upstream routing | Direct forward to requested host | Policy-based routing |

### 1.3 Stubbed Components

Interfaces exist, implementation is a no-op or minimal placeholder.

| Component | Stub Behavior | Future State |
|-----------|--------------|--------------|
| Policy engine | Always returns `allow` | Rule evaluation (G3) |
| Wrapper parser | Passes raw HTTP request through (no envelope) | Full envelope parsing |
| Backpressure | No limits applied | Semaphore + circuit breaker (G3) |
| Hourly aggregation worker | Not running | Background aggregation |
| Off-site backup sync | Manual only | Automated rsync/S3 |
| Monitoring/alerting | Manual log inspection | node_exporter + alerts |
| Certificate renewal | Manual re-enrollment | Automated renewal flow |
| Bulk revocation | Not implemented | Admin API endpoint |

---

## 2. Docker Compose Structure

```yaml
# docker-compose.yml — Minimal Vertical Slice
version: "3.9"

services:
  gateway:
    build:
      context: ./gateway
      dockerfile: Dockerfile
    restart: unless-stopped
    ports:
      - "443:8443"
    volumes:
      - ./certs/ca/ca-chain.pem:/etc/themisto/ca/ca-chain.pem:ro
      - ./certs/server/server.crt:/etc/themisto/server/server.crt:ro
      - ./certs/server/server.key:/etc/themisto/server/server.key:ro
      - ./config/gateway.yaml:/etc/themisto/config.yaml:ro
    environment:
      - DB_DSN=postgres://themisto:${POSTGRES_PASSWORD}@postgres:5432/themisto?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "5"

  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile
    restart: unless-stopped
    ports:
      - "8443:8443"
    volumes:
      - ./certs/ca/ca-chain.pem:/etc/themisto/ca/ca-chain.pem:ro
      - ./certs/ca/issuing-ca.crt:/etc/themisto/ca/issuing-ca.crt:ro
      - ./certs/ca/issuing-ca.key:/etc/themisto/ca/issuing-ca.key:ro
      - ./certs/server/server.crt:/etc/themisto/server/server.crt:ro
      - ./certs/server/server.key:/etc/themisto/server/server.key:ro
      - ./config/backend.yaml:/etc/themisto/config.yaml:ro
    environment:
      - DB_DSN=postgres://themisto:${POSTGRES_PASSWORD}@postgres:5432/themisto?sslmode=disable
      - ADMIN_API_KEY=${ADMIN_API_KEY}
    depends_on:
      postgres:
        condition: service_healthy
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "5"

  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    volumes:
      - pg_data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d:ro
    environment:
      - POSTGRES_USER=themisto
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - POSTGRES_DB=themisto
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U themisto"]
      interval: 5s
      timeout: 3s
      retries: 5
    ports:
      - "127.0.0.1:5432:5432"
    logging:
      driver: json-file
      options:
        max-size: "20m"
        max-file: "3"

volumes:
  pg_data:
```

### Environment File (`.env`, not committed)

```
POSTGRES_PASSWORD=<random-32-char>
ADMIN_API_KEY=<random-32-char-base64url>
```

---

## 3. Directory Layout on VPS

```
/opt/themisto/
├── docker-compose.yml
├── .env                          # secrets, not in git
│
├── gateway/
│   ├── cmd/gateway/main.go
│   ├── internal/...              # minimal gateway code
│   ├── Dockerfile
│   └── go.mod
│
├── backend/
│   ├── cmd/backend/main.go
│   ├── internal/...              # minimal backend code
│   ├── Dockerfile
│   └── go.mod
│
├── migrations/
│   ├── 001_organizations.sql
│   ├── 002_devices.sql
│   ├── 003_enrollment_tokens.sql
│   ├── 004_certificates.sql
│   ├── 005_audit_log.sql
│   ├── 006_policy_rules.sql
│   ├── 007_telemetry_events.sql
│   └── 008_seed_org.sql          # hardcoded org + API key
│
├── certs/
│   ├── ca/
│   │   ├── root-ca.key           # 0400 root:root, NOT in docker
│   │   ├── root-ca.crt
│   │   ├── issuing-ca.key        # 0400 root:root
│   │   ├── issuing-ca.crt
│   │   └── ca-chain.pem
│   └── server/
│       ├── server.crt
│       └── server.key
│
├── config/
│   ├── gateway.yaml
│   └── backend.yaml
│
├── scripts/
│   ├── gen-ca.sh                 # CA generation script
│   ├── gen-server-cert.sh        # Server cert generation
│   ├── pg_backup.sh              # Database backup
│   └── enroll-device.sh          # Manual device enrollment helper
│
└── backups/
    └── ...
```

---

## 4. Minimal Gateway Implementation

For the vertical slice, the gateway is drastically simplified:

```
Agent Request Flow (Minimal):

1. TLS handshake (REAL — mTLS, cert verification)
2. Extract device_id, org_id from client cert (REAL)
3. Check cert revocation status (REAL — query backend)
4. Parse HTTP request (SIMPLIFIED — no wrapper envelope)
5. Policy check (STUB — always allow)
6. Forward to upstream (REAL — direct HTTP forward)
7. Relay response to agent (REAL)
8. Log telemetry event (REAL — batch insert to PostgreSQL)
```

### Modules Included in Minimal Gateway

| Module | Included | Notes |
|--------|----------|-------|
| `tls/` | Full | mTLS listener, cert verification, identity extraction |
| `handler/proxy.go` | Simplified | No wrapper parsing, direct HTTP proxy |
| `handler/health.go` | Full | /healthz endpoint |
| `telemetry/emitter.go` | Full | Async buffer + batch insert |
| `telemetry/buffer.go` | Full | Channel-based buffering |
| `store/postgres.go` | Full | DB connection pool |
| `upstream/forwarder.go` | Simplified | Direct forward, basic timeout |
| `wrapper/` | Skipped | Agent sends raw HTTP for now |
| `policy/` | Stub | `func Apply() { return Allow }` |
| `backpressure/` | Skipped | No limits in minimal slice |
| `telemetry/metrics.go` | Skipped | No Prometheus in minimal slice |

---

## 5. Minimal Backend Implementation

| Endpoint | Included | Notes |
|----------|----------|-------|
| `POST /api/v1/devices` | Full | Device registration |
| `POST /api/v1/devices/{id}/csr` | Full | CSR validation + signing |
| `POST /api/v1/devices/{id}/revoke` | Full | Single device revocation |
| `GET /internal/cert-status/{serial}` | Full | Gateway revocation check |
| `GET /api/v1/orgs/{id}/devices` | Simplified | List devices, no pagination |
| `POST /api/v1/devices/{id}/renew` | Deferred | Not in minimal slice |
| `POST /api/v1/orgs/{id}/revoke-all` | Deferred | Not in minimal slice |

---

## 6. Seed Data (008_seed_org.sql)

```sql
-- Hardcoded org for minimal slice
INSERT INTO organizations (id, name, slug, api_key_hash, status)
VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'Themisto Dev Org',
    'themisto-dev',
    '$2a$10$...', -- bcrypt hash of ADMIN_API_KEY from .env
    'active'
);
```

The seed script uses a placeholder. On first deploy, run a one-time script to hash the actual API key and update the row.

---

## 7. Acceptance Criteria

### 7.1 End-to-End Flow

The minimal slice is accepted when all of the following pass:

| # | Criterion | How to Verify |
|---|-----------|---------------|
| 1 | CA key pair generated on VPS | `openssl x509 -in root-ca.crt -text` shows correct subject/algorithm |
| 2 | Server cert issued by CA | `openssl verify -CAfile ca-chain.pem server.crt` returns OK |
| 3 | Docker Compose starts all 3 services | `docker compose up -d && docker compose ps` shows all healthy |
| 4 | PostgreSQL schema applied | Connect to DB, `\dt` shows all tables |
| 5 | Device registered via API | `curl -X POST /api/v1/devices` returns 201 with enrollment token |
| 6 | CSR submitted, cert issued | Submit PKCS#10 CSR, receive signed cert + CA chain |
| 7 | Agent connects to gateway with mTLS | `curl --cert client.crt --key client.key --cacert ca-chain.pem https://gateway/healthz` returns 200 |
| 8 | Agent request forwarded to upstream | Agent proxies `https://httpbin.org/get` through gateway, receives valid response |
| 9 | Telemetry event stored | Query `telemetry_events` table, find the proxied request logged |
| 10 | Certificate revocation works | Revoke cert via API, verify agent connection is rejected |
| 11 | Audit log populated | Query `audit_log` table, find `cert.issued` and `cert.revoked` entries |
| 12 | /healthz accessible without mTLS | `curl https://gateway/healthz` (no client cert) returns 200 |

### 7.2 Security Criteria

| # | Criterion |
|---|-----------|
| S1 | CA key is 0400 root:root, not in any container |
| S2 | TLS 1.3 enforced (TLS 1.2 connection fails) |
| S3 | Connection without client cert is rejected |
| S4 | Connection with self-signed cert (not from Themisto CA) is rejected |
| S5 | Revoked cert is rejected within 60 seconds |
| S6 | SSH is key-only, root login disabled |
| S7 | UFW active, only ports 22/443/8443 open |
| S8 | PostgreSQL not accessible from outside (127.0.0.1 only) |

### 7.3 Operational Criteria

| # | Criterion |
|---|-----------|
| O1 | `docker compose down && docker compose up -d` — services recover, data persists |
| O2 | Kill gateway container — Docker restarts it automatically |
| O3 | PostgreSQL backup script runs, produces encrypted dump |
| O4 | Backup can be restored to a fresh PostgreSQL instance |

---

## 8. What Is Explicitly Excluded

| Feature | Reason |
|---------|--------|
| Dashboard / Web UI | Out of scope — CLI and API only |
| DLP (Data Loss Prevention) | Future feature — requires policy engine |
| Horizontal scaling | Single VPS is sufficient for validation |
| Multi-region | Single VPS deployment |
| Load balancer | Direct port mapping |
| CI/CD pipeline | Manual deploy for now |
| Agent auto-update | Manual agent builds |
| Multi-org | Hardcoded single org |
| Certificate renewal | Manual re-enrollment |
| Prometheus / Grafana | Manual log/DB inspection |
| Rate limiting | No backpressure in minimal slice |
| Wrapper envelope protocol | Agent sends raw HTTP |
| Policy management API | Hardcoded allow-all |

---

## 9. Deployment Sequence

Step-by-step to go from bare VPS to working vertical slice:

### Phase 1: VPS Setup (G1)

1. Provision Ubuntu 24.04 LTS VPS (2 vCPU, 4 GB RAM, 80 GB SSD)
2. Run hardening checklist (G1 Sections 2-3)
3. Install Docker Engine + Compose v2
4. Create `/opt/themisto/` directory structure

### Phase 2: Certificate Setup (G2)

5. Generate root CA key + cert (`scripts/gen-ca.sh`)
6. Generate issuing CA key + cert (signed by root)
7. Generate server cert for gateway (`scripts/gen-server-cert.sh`)
8. Set file permissions (CA keys: 0400 root:root)
9. Back up root CA key to offline storage

### Phase 3: Database

10. Create `.env` with generated passwords and API key
11. `docker compose up -d postgres`
12. Verify migrations applied: `docker exec ... psql -c '\dt'`
13. Seed organization: run seed script or manual INSERT

### Phase 4: Backend

14. Build backend image: `docker compose build backend`
15. `docker compose up -d backend`
16. Verify health: `curl http://localhost:8443/healthz`
17. Test device registration: `curl -X POST .../devices`
18. Test CSR submission + cert issuance

### Phase 5: Gateway

19. Build gateway image: `docker compose build gateway`
20. `docker compose up -d gateway`
21. Verify health: `curl -k https://localhost/healthz`
22. Test mTLS connection with issued client cert
23. Test request forwarding through gateway

### Phase 6: End-to-End Validation

24. Run full acceptance criteria (Section 7.1, items 1-12)
25. Run security criteria (Section 7.2, items S1-S8)
26. Run operational criteria (Section 7.3, items O1-O4)
27. Set up backup cron job
28. Document any deviations or issues

### Phase 7: Agent Integration

29. Configure agent with gateway URL + client cert
30. Agent sets system proxy
31. Browse to `https://httpbin.org/get` through agent
32. Verify request appears in `telemetry_events` table
33. Vertical slice complete
