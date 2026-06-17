# G4 — Backend Device Enrollment & Certificate Issuance

Backend service for device registration, CSR intake, certificate issuance, renewal, revocation, and org-device association.

---

## 1. API Endpoints

### 1.1 Device Registration

```
POST /api/v1/devices
```

Register a new device in an organization. Returns a one-time enrollment token.

**Request:**
```json
{
  "org_id": "uuid",
  "device_name": "akshat-macbook",
  "os": "darwin",
  "agent_version": "0.1.0"
}
```

**Response (201):**
```json
{
  "device_id": "uuid",
  "enrollment_token": "base64-token",
  "token_expires_at": "2025-02-01T12:00:00Z"
}
```

**Auth:** Org admin API key (header `Authorization: Bearer <org-api-key>`).

---

### 1.2 CSR Submission & Certificate Issuance

```
POST /api/v1/devices/{device_id}/csr
```

Submit a CSR to obtain a client certificate.

**Request:**
```
Content-Type: application/pkcs10
X-Enrollment-Token: <enrollment-token>

-----BEGIN CERTIFICATE REQUEST-----
...
-----END CERTIFICATE REQUEST-----
```

**Response (200):**
```json
{
  "certificate": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
  "ca_chain": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
  "not_before": "2025-01-15T00:00:00Z",
  "not_after": "2025-04-15T00:00:00Z",
  "serial": "a1b2c3d4..."
}
```

**Errors:** See G2, Section 3.2 for CSR validation error codes.

---

### 1.3 Certificate Renewal

```
POST /api/v1/devices/{device_id}/renew
```

Renew an expiring certificate. Must be called with a valid (not yet expired) client cert via mTLS.

**Auth:** mTLS — the device authenticates with its current valid certificate.

**Request:**
```
Content-Type: application/pkcs10

-----BEGIN CERTIFICATE REQUEST-----
...
-----END CERTIFICATE REQUEST-----
```

**Response (200):** Same as CSR submission response. The old certificate is revoked after the new one is confirmed active.

**Constraints:**
- Renewal allowed only within 30 days of expiration.
- New CSR must use the same CN (device_id) and O (org_id).
- Key may be rotated (new key pair in CSR is allowed).

---

### 1.4 Certificate Revocation

```
POST /api/v1/devices/{device_id}/revoke
```

Revoke a device's active certificate.

**Auth:** Org admin API key.

**Request:**
```json
{
  "reason": "device_decommissioned",
  "serial": "a1b2c3d4..."
}
```

**Response (200):**
```json
{
  "device_id": "uuid",
  "serial": "a1b2c3d4...",
  "status": "revoked",
  "revoked_at": "2025-01-20T10:00:00Z"
}
```

**Revocation Reasons (enum):**
- `device_decommissioned`
- `key_compromise`
- `org_offboarded`
- `policy_violation`
- `superseded` (renewal)
- `admin_action`

---

### 1.5 Bulk Revocation (Org)

```
POST /api/v1/orgs/{org_id}/revoke-all
```

Revoke all active certificates for an organization.

**Auth:** Super admin API key.

**Response (200):**
```json
{
  "org_id": "uuid",
  "revoked_count": 47,
  "revoked_at": "2025-01-20T10:00:00Z"
}
```

---

### 1.6 Certificate Status (Internal)

```
GET /internal/cert-status/{serial}
```

Used by the gateway to check certificate validity during mTLS handshake.

**Auth:** Internal network only (no external access).

**Response (200):**
```json
{
  "serial": "a1b2c3d4...",
  "status": "active",
  "device_id": "uuid",
  "org_id": "uuid"
}
```

Status values: `active`, `revoked`, `expired`.

---

### 1.7 Device Listing

```
GET /api/v1/orgs/{org_id}/devices?status=active&page=1&per_page=50
```

List devices for an organization.

**Auth:** Org admin API key.

**Response (200):**
```json
{
  "devices": [
    {
      "device_id": "uuid",
      "device_name": "akshat-macbook",
      "os": "darwin",
      "status": "active",
      "cert_serial": "a1b2c3d4...",
      "cert_expires_at": "2025-04-15T00:00:00Z",
      "enrolled_at": "2025-01-15T00:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "per_page": 50
}
```

---

## 2. Database Schema

### 2.1 Organizations

```sql
CREATE TABLE organizations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL UNIQUE,
    api_key_hash    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'suspended', 'offboarded')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_organizations_slug ON organizations(slug);
```

### 2.2 Devices

```sql
CREATE TABLE devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    device_name     TEXT NOT NULL,
    os              TEXT NOT NULL CHECK (os IN ('darwin', 'windows', 'linux')),
    agent_version   TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'active', 'suspended', 'decommissioned')),
    enrolled_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (org_id, device_name)
);

CREATE INDEX idx_devices_org_id ON devices(org_id);
CREATE INDEX idx_devices_status ON devices(status);
```

### 2.3 Enrollment Tokens

```sql
CREATE TABLE enrollment_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID NOT NULL REFERENCES devices(id),
    token_hash      TEXT NOT NULL UNIQUE,
    used            BOOLEAN NOT NULL DEFAULT false,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_enrollment_tokens_device_id ON enrollment_tokens(device_id);
CREATE INDEX idx_enrollment_tokens_expires ON enrollment_tokens(expires_at)
    WHERE used = false;
```

### 2.4 Certificates

```sql
CREATE TABLE certificates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID NOT NULL REFERENCES devices(id),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    serial          TEXT NOT NULL UNIQUE,
    subject_cn      TEXT NOT NULL,
    subject_o       TEXT NOT NULL,
    issuer_cn       TEXT NOT NULL,
    key_algorithm   TEXT NOT NULL,
    not_before      TIMESTAMPTZ NOT NULL,
    not_after       TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'revoked', 'expired')),
    revoked_at      TIMESTAMPTZ,
    revocation_reason TEXT,
    csr_pem         TEXT NOT NULL,
    cert_pem        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_certificates_device_id ON certificates(device_id);
CREATE INDEX idx_certificates_org_id ON certificates(org_id);
CREATE INDEX idx_certificates_serial ON certificates(serial);
CREATE INDEX idx_certificates_status ON certificates(status);
CREATE INDEX idx_certificates_not_after ON certificates(not_after)
    WHERE status = 'active';
```

### 2.5 Audit Log

```sql
CREATE TABLE audit_log (
    id              BIGSERIAL PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type      TEXT NOT NULL CHECK (actor_type IN ('admin', 'device', 'system')),
    actor_id        TEXT NOT NULL,
    org_id          UUID REFERENCES organizations(id),
    action          TEXT NOT NULL,
    resource_type   TEXT NOT NULL,
    resource_id     TEXT NOT NULL,
    details         JSONB,
    ip_address      INET
);

CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_log_org_id ON audit_log(org_id);
CREATE INDEX idx_audit_log_action ON audit_log(action);
CREATE INDEX idx_audit_log_resource ON audit_log(resource_type, resource_id);
```

### 2.6 Policy Rules

```sql
CREATE TABLE policy_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    priority        INTEGER NOT NULL,
    match_host      TEXT,
    match_path      TEXT,
    match_method    TEXT,
    decision        TEXT NOT NULL CHECK (decision IN ('allow', 'block', 'log_only')),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (org_id, priority)
);

CREATE INDEX idx_policy_rules_org_id ON policy_rules(org_id)
    WHERE enabled = true;
```

---

## 3. Validation Rules

### 3.1 Device Registration

| Field | Rule |
|-------|------|
| `org_id` | Must exist in `organizations` with status `active` |
| `device_name` | 1-128 chars, alphanumeric + hyphens, unique per org |
| `os` | Must be `darwin`, `windows`, or `linux` |
| `agent_version` | Semver format (validated but not enforced) |

### 3.2 CSR Validation

See G2, Section 3 for full CSR validation rules. Summary:
- Valid PKCS#10 signature.
- CN matches device_id.
- O matches org name.
- ECDSA P-256 or P-384 key.
- No duplicate active certs.
- Valid, unused, unexpired enrollment token.

### 3.3 API Key Validation

- API keys are generated as 32-byte random values, base64url-encoded.
- Stored as bcrypt hash in `organizations.api_key_hash`.
- Compared on every request via `Authorization: Bearer <key>`.
- Rate limited: 100 requests/minute per API key.

### 3.4 Input Sanitization

- All text inputs trimmed of leading/trailing whitespace.
- SQL injection prevented via parameterized queries (no string interpolation).
- JSON request body size limited to 1 MB.
- CSR body size limited to 16 KB.

---

## 4. Audit Logging

### 4.1 Audited Actions

| Action | Actor | Resource | Details |
|--------|-------|----------|---------|
| `device.registered` | admin | device | device_name, os |
| `device.decommissioned` | admin | device | reason |
| `enrollment_token.created` | system | enrollment_token | device_id, expires_at |
| `enrollment_token.used` | device | enrollment_token | device_id |
| `csr.submitted` | device | certificate | key_algorithm, subject |
| `csr.rejected` | system | certificate | rejection_reason |
| `cert.issued` | system | certificate | serial, not_after |
| `cert.renewed` | device | certificate | old_serial, new_serial |
| `cert.revoked` | admin/system | certificate | serial, reason |
| `org.bulk_revoked` | admin | organization | revoked_count |
| `cert_status.checked` | gateway | certificate | serial, status |

### 4.2 Audit Log Properties

- **Append-only:** No updates or deletes on the audit_log table.
- **Immutable:** Application code never modifies existing rows.
- **Retention:** 1 year minimum. Archive to cold storage after 90 days (future).
- **Query patterns:** By org_id + time range, by resource_id, by action.

### 4.3 Implementation

Every API handler wraps its database transaction to include an audit log INSERT. The audit log write is part of the same transaction as the business operation, ensuring consistency.

```
BEGIN;
  -- Business operation (e.g., INSERT certificate)
  INSERT INTO certificates (...) VALUES (...);
  -- Audit log
  INSERT INTO audit_log (actor_type, actor_id, org_id, action, resource_type, resource_id, details)
  VALUES ('device', $device_id, $org_id, 'cert.issued', 'certificate', $serial, $details);
COMMIT;
```

---

## 5. Service Architecture

```
backend/
├── cmd/
│   └── backend/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── api/
│   │   ├── router.go         # HTTP router setup
│   │   ├── middleware.go      # Auth, logging, rate limiting
│   │   ├── device.go          # Device registration handlers
│   │   ├── enrollment.go      # CSR + issuance handlers
│   │   ├── revocation.go      # Revocation handlers
│   │   ├── cert_status.go     # Internal cert status handler
│   │   └── org.go             # Org-level operations
│   ├── signing/
│   │   ├── signer.go          # CA signing operations
│   │   └── csr_validator.go   # CSR validation logic
│   ├── store/
│   │   ├── postgres.go        # DB connection
│   │   ├── device.go          # Device queries
│   │   ├── certificate.go     # Certificate queries
│   │   ├── enrollment.go      # Enrollment token queries
│   │   ├── audit.go           # Audit log queries
│   │   └── policy.go          # Policy rule queries
│   └── token/
│       └── generator.go       # Enrollment token generation
├── migrations/
│   ├── 001_create_organizations.sql
│   ├── 002_create_devices.sql
│   ├── 003_create_enrollment_tokens.sql
│   ├── 004_create_certificates.sql
│   ├── 005_create_audit_log.sql
│   └── 006_create_policy_rules.sql
├── go.mod
├── Dockerfile
└── Makefile
```

---

## 6. Certificate Lifecycle State Machine

```
                ┌──────────┐
                │ pending  │  (device registered, no cert yet)
                └────┬─────┘
                     │ CSR submitted + signed
                     ▼
                ┌──────────┐
          ┌─────│  active  │◄────────────┐
          │     └────┬─────┘             │
          │          │                   │
          │  revoke  │  renew            │ new cert issued
          │          │                   │
          ▼          ▼                   │
    ┌──────────┐  ┌──────────┐     ┌────┴─────┐
    │ revoked  │  │ renewing │────►│  active   │
    └──────────┘  └──────────┘     │ (new cert)│
                                   └──────────┘
                                        │
                                  time passes
                                        │
                                        ▼
                                   ┌──────────┐
                                   │ expired  │
                                   └──────────┘
```

A cron job or background worker transitions `active` certificates to `expired` when `not_after < now()`.
