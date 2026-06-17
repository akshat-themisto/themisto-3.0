# G2 — Certificate Authority & mTLS Server Design

Complete CA and mTLS server architecture for the Themisto gateway on Ubuntu.

---

## 1. CA Key Generation

### 1.1 Root CA

```
Algorithm:    ECDSA P-384
Validity:     10 years
Subject:      CN=Themisto Root CA, O=<org>
Key Usage:    Certificate Sign, CRL Sign
Basic Const:  CA:TRUE, pathlen:1
```

Generation procedure (performed on the VPS or a secure workstation):

1. Generate private key in a tmpfs or encrypted partition.
2. Generate self-signed root certificate.
3. Immediately back up the key (see Section 7).
4. Restrict permissions on the host copy.

Why ECDSA P-384: Equivalent security to RSA-3072+ with smaller keys and faster operations. P-384 is NIST-approved, widely supported by Go's `crypto/tls`, and future-resistant for the CA lifetime.

### 1.2 Intermediate CA (Optional, Recommended)

```
Algorithm:    ECDSA P-256
Validity:     3 years
Subject:      CN=Themisto Issuing CA, O=<org>
Key Usage:    Certificate Sign, CRL Sign
Basic Const:  CA:TRUE, pathlen:0
Issuer:       Themisto Root CA
```

Using an intermediate CA allows the root key to stay offline. The intermediate key is the one actively used for signing device/server certificates. If compromised, only the intermediate is revoked; the root survives.

For the minimal deployment (G6), the root CA may sign directly. The intermediate is added when scaling.

---

## 2. Secure Storage & Permissions

### 2.1 File Layout on Host

```
/etc/themisto/ca/
├── root-ca.key          # 0400 root:root — NEVER in container
├── root-ca.crt          # 0444 root:root — public, mounted RO
├── issuing-ca.key       # 0400 root:root — signing container only
├── issuing-ca.crt       # 0444 root:root — public, mounted RO
└── ca-chain.pem         # 0444 root:root — root + issuing certs
```

### 2.2 Key Protection

| Control | Implementation |
|---------|---------------|
| File permissions | `chmod 0400`, `chown root:root` |
| Directory permissions | `/etc/themisto/ca` is `0500 root:root` |
| No container access to root key | Root key never bind-mounted |
| Signing container | Short-lived, bind-mounts only issuing key |
| Disk encryption | LUKS on the partition or full-disk encryption |
| In-memory only (future) | HSM or PKCS#11 integration |

### 2.3 Environment Isolation

- The CA key file is **not** in the Docker Compose working directory.
- The signing service (backend) receives the issuing CA key via a restricted bind-mount with `:ro` removed only for the signing path.
- If the VPS is cloud-hosted, enable disk encryption (provider-level or LUKS).

---

## 3. CSR Validation

When a device submits a Certificate Signing Request:

### 3.1 Validation Steps

1. **Parse & verify CSR signature** — reject malformed or unsigned CSRs.
2. **Check Subject fields:**
   - `CN` must match the registered device ID in the enrollment database.
   - `O` must match the organization the device belongs to.
   - No wildcard CNs allowed.
3. **Check key algorithm & size:**
   - Accept: ECDSA P-256 or P-384 only.
   - Reject: RSA (too large for agent use), DSA, Ed25519 (until gateway supports it).
4. **Check for duplicate CNs** — if a valid certificate already exists for this CN, require explicit renewal or revocation first.
5. **Check enrollment token** — the CSR submission must include a valid, unexpired enrollment token (one-time use, bound to the device ID).
6. **Rate limit** — max 5 CSR submissions per device per hour.

### 3.2 Rejection Responses

| Failure | HTTP Status | Error Code |
|---------|-------------|------------|
| Malformed CSR | 400 | `CSR_INVALID` |
| Unknown device ID | 404 | `DEVICE_NOT_FOUND` |
| Org mismatch | 403 | `ORG_MISMATCH` |
| Weak key | 400 | `KEY_ALGORITHM_REJECTED` |
| Duplicate active cert | 409 | `CERT_ALREADY_ACTIVE` |
| Invalid/expired token | 401 | `TOKEN_INVALID` |
| Rate limited | 429 | `RATE_LIMITED` |

---

## 4. Certificate Signing

### 4.1 Issued Certificate Profile

```
Algorithm:     Same as CSR key (ECDSA P-256 or P-384)
Validity:      90 days (configurable per org)
Subject:       From validated CSR (CN=device-id, O=org)
Key Usage:     Digital Signature
Ext Key Usage: Client Authentication
SAN:           None (identity is CN-based, not hostname-based)
Serial:        Cryptographically random 128-bit integer
Issuer:        Themisto Issuing CA (or Root CA in minimal mode)
```

### 4.2 Signing Procedure

1. Backend validates CSR (Section 3).
2. Backend calls internal signing function (or dedicated signing microservice).
3. Signing function loads issuing CA key + cert.
4. Signs the CSR → produces X.509 certificate.
5. Stores certificate metadata in database (serial, device_id, org_id, not_before, not_after, status).
6. Returns PEM-encoded certificate + CA chain to the device.
7. Emits audit event: `cert.issued`.

### 4.3 Serial Number Management

- Serials are 128-bit cryptographically random (`crypto/rand`).
- Stored in database as hex string.
- Uniqueness enforced by DB unique constraint on `serial`.

---

## 5. Revocation Strategy

### 5.1 Revocation Triggers

| Trigger | Actor | Mechanism |
|---------|-------|-----------|
| Device decommissioned | Admin API | Explicit revocation |
| Key compromise suspected | Admin API or automated | Explicit revocation |
| Org offboarded | Admin API (bulk) | Revoke all certs for org |
| Certificate renewal | Device (automatic) | Old cert revoked after new cert confirmed |
| Policy violation detected | Gateway (automated) | Revoke + block |

### 5.2 Revocation Database

```sql
-- certificates table includes:
--   status: 'active' | 'revoked' | 'expired'
--   revoked_at: timestamp
--   revocation_reason: text
```

### 5.3 Revocation Checking

**Primary: Online check (OCSP-like).** The gateway checks certificate status against the backend database on every mTLS handshake (or with a short TTL cache of 60s). This avoids CRL distribution complexity.

```
Agent connects → TLS handshake → Gateway extracts client cert serial →
  Gateway queries backend: GET /internal/cert-status/{serial} →
  Backend returns { "status": "active" | "revoked" } →
  Gateway accepts or terminates connection
```

**Cache:** Gateway maintains an in-memory cache of cert statuses (serial → status) with 60-second TTL. On cache miss, queries backend synchronously. Revocation is eventual-consistent within 60 seconds.

### 5.4 CRL (Future)

If the number of devices grows large (>10k), publish a CRL:
- Signed by issuing CA.
- Published to a well-known URL.
- Agents and gateway can optionally check CRL for offline scenarios.
- Not required for the minimal deployment.

---

## 6. Gateway TLS Configuration

### 6.1 Server-side TLS

```
Min TLS version:   TLS 1.3 (TLS 1.2 as fallback only if required)
Cipher suites:     TLS_AES_256_GCM_SHA384, TLS_CHACHA20_POLY1305_SHA256, TLS_AES_128_GCM_SHA256
Server cert:       ECDSA P-256 server certificate (signed by issuing CA or public CA)
Server key:        Corresponding ECDSA private key
Client auth:       RequireAndVerifyClientCert
Client CA pool:    Themisto CA chain (root + issuing)
```

### 6.2 mTLS Enforcement

```
                    ┌──────────────┐
                    │   Gateway    │
                    │              │
Agent ──mTLS──────► │ TLS Listener │
  client cert       │  - Verify    │
  from Themisto CA  │    client CA │
                    │  - Check     │
                    │    serial    │
                    │    status    │
                    └──────────────┘
```

- Every connection must present a valid client certificate signed by Themisto CA.
- The gateway extracts `CN` (device ID) and `O` (org) from the client cert for routing and policy.
- Connections without client certs are rejected at the TLS level (no HTTP response).
- Exception: `/healthz` endpoint is plain TLS (no mTLS) for uptime monitors.

### 6.3 Go TLS Config (Conceptual)

```go
tlsConfig := &tls.Config{
    MinVersion:   tls.VersionTLS13,
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    caCertPool,
    Certificates: []tls.Certificate{serverCert},
}
```

---

## 7. Disaster Recovery Plan

### 7.1 Scenarios & Response

| Scenario | Impact | Recovery |
|----------|--------|----------|
| VPS destroyed | All services down | Provision new VPS, restore from backups |
| DB corrupted | Enrollment data lost | Restore from latest pg_dump |
| Issuing CA key lost | Cannot sign new certs | Restore from encrypted backup |
| Root CA key lost | Cannot issue new issuing CA | Restore from offline backup (USB/safe) |
| Server cert expired | Gateway unreachable | Re-issue from issuing CA (automated renewal recommended) |

### 7.2 Recovery Procedure

**Full VPS Recovery (worst case):**

1. Provision new Ubuntu 24.04 LTS VPS.
2. Run hardening checklist (G1).
3. Restore CA keys from offline encrypted backup.
4. Restore database from latest encrypted pg_dump.
5. Deploy Docker Compose stack (images from registry).
6. Verify: gateway accepts mTLS connections, enrolled devices can connect.
7. Update DNS / IP if VPS address changed.

**Target RTO:** 2 hours (manual), 30 minutes (with automation scripts).
**Target RPO:** 24 hours (daily backups), configurable down to 1 hour with WAL archiving.

### 7.3 Backup Inventory

| Asset | Location 1 | Location 2 | Frequency |
|-------|-----------|-----------|-----------|
| Root CA key (encrypted) | USB in safe | Encrypted cloud storage | On creation + rotation |
| Issuing CA key (encrypted) | USB in safe | Encrypted cloud storage | On creation + rotation |
| PostgreSQL dump (encrypted) | VPS `/opt/themisto/backups` | Off-site object storage | Daily |
| Docker Compose + config | Git repository | — | On change |
| Server TLS cert | Git or secrets manager | — | On renewal |

---

## 8. Compromise Response Plan

### 8.1 Severity Levels

| Level | Description | Example |
|-------|-------------|---------|
| SEV-1 | Root CA key compromised | Key exfiltrated, attacker can issue arbitrary certs |
| SEV-2 | Issuing CA key compromised | Attacker can issue device certs |
| SEV-3 | Server TLS key compromised | Attacker can impersonate gateway |
| SEV-4 | Single device cert compromised | Attacker can impersonate one device |
| SEV-5 | VPS access compromised (no key access) | Attacker on system but keys encrypted/inaccessible |

### 8.2 Response Procedures

**SEV-1: Root CA Compromise**

1. **Immediate:** Take gateway offline. Notify all org admins.
2. Generate new root CA on a clean, air-gapped machine.
3. Issue new issuing CA under the new root.
4. Re-enroll all devices with new certificates.
5. Deploy new CA chain to gateway.
6. Restore gateway service.
7. Post-incident: forensic analysis of how root key was accessed.

**SEV-2: Issuing CA Compromise**

1. **Immediate:** Revoke the issuing CA certificate.
2. Generate new issuing CA signed by root (root key is offline/safe).
3. Revoke all certificates issued by the compromised issuing CA.
4. Re-enroll all devices.
5. Deploy new CA chain to gateway.
6. Post-incident review.

**SEV-3: Server TLS Key Compromise**

1. Revoke server certificate (if using public CA with OCSP).
2. Generate new server key and CSR.
3. Issue new server certificate.
4. Deploy to gateway, restart.
5. Minimal device impact (mTLS client certs unaffected).

**SEV-4: Single Device Compromise**

1. Revoke the device's certificate (admin API or automated).
2. Gateway rejects the device within cache TTL (≤60s).
3. Investigate the device; re-enroll after remediation.

**SEV-5: VPS Access Compromise**

1. Assess whether keys were accessed (check file access times, audit logs).
2. If keys potentially accessed → escalate to SEV-2 or SEV-3.
3. If keys safe → rotate SSH keys, patch vulnerability, review access logs.
4. Consider re-provisioning VPS from scratch (treat as contaminated).

### 8.3 Communication Plan

- SEV-1/SEV-2: Notify all customers/orgs within 1 hour.
- SEV-3/SEV-4: Notify affected org within 4 hours.
- SEV-5: Internal notification; customer notification if data exposure suspected.
- All incidents: written post-mortem within 72 hours.
