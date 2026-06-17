# G1 — Ubuntu VPS Hardening Plan

Production-ready Ubuntu VPS setup for hosting the Themisto gateway and backend services.

---

## 1. Ubuntu LTS Recommendation

| Choice | Version | EOL (Standard / ESM) |
|--------|---------|----------------------|
| **Primary** | Ubuntu 24.04 LTS (Noble Numbat) | Apr 2029 / Apr 2034 |
| Fallback | Ubuntu 22.04 LTS (Jammy Jellyfish) | Apr 2027 / Apr 2032 |

Use **24.04 LTS** for new deployments. Kernel 6.8+ ships with io_uring hardening and improved eBPF support. Pin to HWE kernel for driver updates without full dist-upgrade.

```
apt install --install-recommends linux-generic-hwe-24.04
```

---

## 2. UFW Firewall Rules

### 2.1 Default Policy

```bash
ufw default deny incoming
ufw default allow outgoing
ufw logging on
```

### 2.2 Allow Rules

| Port | Proto | Source | Purpose |
|------|-------|--------|---------|
| 22 | TCP | Admin CIDR only | SSH (rate limited) |
| 443 | TCP | Any | Gateway mTLS listener |
| 8443 | TCP | Any | Backend enrollment API (if separate) |

```bash
ufw limit from <ADMIN_CIDR> to any port 22 proto tcp
ufw allow 443/tcp
ufw allow 8443/tcp
ufw enable
```

### 2.3 Hardening Extras

- Drop ICMP echo on production (optional, breaks some monitoring):
  ```
  # /etc/ufw/before.rules — change ACCEPT to DROP for icmp echo-request
  ```
- Block outbound to metadata endpoints (169.254.169.254) if running on cloud.

---

## 3. SSH Hardening

### 3.1 `/etc/ssh/sshd_config` changes

```
Port 22
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
AuthorizedKeysFile .ssh/authorized_keys
MaxAuthTries 3
LoginGraceTime 30
ClientAliveInterval 300
ClientAliveCountMax 2
AllowUsers deploy
X11Forwarding no
AllowTcpForwarding no
PermitTunnel no
```

### 3.2 Checklist

- [ ] Create `deploy` user with sudo, disable root password
- [ ] Copy authorized_keys for all operators
- [ ] Remove host keys and regenerate: `ssh-keygen -A`
- [ ] Test login before restarting sshd
- [ ] Install `fail2ban` with SSH jail enabled
  ```bash
  apt install fail2ban
  systemctl enable fail2ban
  ```

---

## 4. Docker vs systemd — Rationale

| Factor | Docker Compose | Bare systemd |
|--------|---------------|--------------|
| Isolation | cgroups + namespaces per container | Process-level only |
| Reproducibility | Image pinning, `docker-compose.yml` in git | Depends on host packages |
| Secrets | Docker secrets / env-file | systemd `LoadCredential` |
| Upgrades | Pull new image, restart | apt upgrade, config drift risk |
| Debugging | `docker logs`, `docker exec` | `journalctl`, direct access |
| Performance overhead | Negligible for network services | None |

**Decision: Docker Compose.** The gateway, backend, and PostgreSQL run as containers. Benefits: hermetic builds, rollback via image tags, single `docker compose up -d` deployment. systemd manages only Docker daemon and host-level services (sshd, ufw, fail2ban, unattended-upgrades).

---

## 5. Docker Compose Structure

```yaml
# docker-compose.yml (production)
version: "3.9"

services:
  gateway:
    image: themisto/gateway:${TAG:-latest}
    restart: unless-stopped
    ports:
      - "443:8443"
    volumes:
      - ./certs/ca:/etc/themisto/ca:ro
      - ./certs/server:/etc/themisto/server:ro
    env_file: .env.gateway
    depends_on:
      postgres:
        condition: service_healthy
    logging:
      driver: json-file
      options:
        max-size: "50m"
        max-file: "5"

  backend:
    image: themisto/backend:${TAG:-latest}
    restart: unless-stopped
    ports:
      - "8443:8443"
    volumes:
      - ./certs/ca:/etc/themisto/ca:ro
      - ./certs/server:/etc/themisto/server:ro
    env_file: .env.backend
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
      - ./backups:/backups
    env_file: .env.postgres
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U themisto"]
      interval: 10s
      timeout: 5s
      retries: 5
    logging:
      driver: json-file
      options:
        max-size: "20m"
        max-file: "3"

volumes:
  pg_data:
```

### Environment Files (not committed)

- `.env.gateway` — `GATEWAY_CA_PATH`, `GATEWAY_CERT_PATH`, `GATEWAY_KEY_PATH`, `DB_DSN`
- `.env.backend` — `DB_DSN`, `CA_KEY_PATH`, `ENROLLMENT_SECRET`
- `.env.postgres` — `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

---

## 6. Log Rotation

### 6.1 Docker JSON Logs

Handled by Docker `json-file` driver config (see Compose above): `max-size: 50m`, `max-file: 5`.

### 6.2 Host-level Logs

```bash
apt install logrotate   # usually pre-installed
```

`/etc/logrotate.d/themisto-host`:
```
/var/log/themisto/*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 0640 root adm
}
```

### 6.3 Application-level

Gateway and backend write structured JSON to stdout (captured by Docker). No separate file logging inside containers.

---

## 7. PostgreSQL Backup Strategy

### 7.1 Automated Daily Backup

`/opt/themisto/scripts/pg_backup.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail

BACKUP_DIR="/opt/themisto/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
CONTAINER="themisto-postgres-1"

mkdir -p "$BACKUP_DIR"
docker exec "$CONTAINER" pg_dump -U themisto -Fc themisto \
  > "$BACKUP_DIR/themisto_${TIMESTAMP}.dump"

# Encrypt backup
gpg --batch --yes --symmetric --cipher-algo AES256 \
  --passphrase-file /etc/themisto/backup.key \
  "$BACKUP_DIR/themisto_${TIMESTAMP}.dump"

rm "$BACKUP_DIR/themisto_${TIMESTAMP}.dump"

# Prune backups older than 30 days
find "$BACKUP_DIR" -name "*.dump.gpg" -mtime +30 -delete
```

### 7.2 Cron Schedule

```
0 2 * * * /opt/themisto/scripts/pg_backup.sh >> /var/log/themisto/pg_backup.log 2>&1
```

### 7.3 Off-site Copy

- Sync encrypted backups to object storage (S3-compatible) or secondary VPS via rsync-over-SSH.
- Verify restore monthly: spin up temp container, restore dump, run sanity queries.

---

## 8. CA Key Backup Strategy

### 8.1 Storage

- CA private key lives on host at `/etc/themisto/ca/ca.key` with permissions `0400 root:root`.
- Key is **never** mounted into containers; only the CA certificate (`ca.crt`) is mounted read-only.
- Signing operations run on the host or in a dedicated short-lived container with the key bind-mounted.

### 8.2 Backup

- Encrypt CA key with GPG to a separate passphrase (stored offline, e.g. password manager or printed).
- Store encrypted copy in **two** geographically separate locations:
  1. Encrypted USB in a safe/lockbox.
  2. Encrypted object storage bucket (separate cloud account).
- **Never** store the CA key in the same backup pipeline as database dumps.

### 8.3 Access Control

- Only the `root` user on the VPS can read the CA key.
- Operators access the VPS via the `deploy` user (no direct root SSH).
- `sudo` to root is logged via `auth.log` and auditable.

---

## 9. Basic Monitoring

### 9.1 Host Metrics

Install **node_exporter** (Prometheus) as a systemd unit (not Docker, to survive Docker failures):

```bash
useradd --no-create-home --shell /usr/sbin/nologin node_exporter
# Download binary, install to /usr/local/bin
# Create /etc/systemd/system/node_exporter.service
systemctl enable --now node_exporter
```

Bind to `127.0.0.1:9100`. Expose via SSH tunnel or internal network only.

### 9.2 Docker Health

- Docker Compose `healthcheck` on PostgreSQL (see above).
- Cron script every 5 min checking `docker compose ps --format json` for unhealthy/exited containers. Alert via webhook or email.

### 9.3 Disk Usage

```bash
# /opt/themisto/scripts/disk_check.sh
THRESHOLD=85
USAGE=$(df / --output=pcent | tail -1 | tr -d ' %')
if [ "$USAGE" -ge "$THRESHOLD" ]; then
  echo "ALERT: Disk usage at ${USAGE}%" | mail -s "Themisto Disk Alert" ops@example.com
fi
```

Cron: every 15 min.

### 9.4 Uptime / Endpoint Check

External uptime monitor (UptimeRobot, Hetrix, or self-hosted) hitting:
- `https://<gateway>:443/healthz` (TLS, no mTLS required for health endpoint)
- Alert on 3 consecutive failures.

### 9.5 Log Alerting (Minimal)

`grep` cron or `journalctl --since` for critical patterns:
- `FATAL`, `panic`, `certificate expired`, `disk full`
- Pipe matches to alerting webhook.

---

## 10. Deployment Checklist

### Pre-deployment

- [ ] Provision Ubuntu 24.04 LTS VPS (minimum 2 vCPU, 4 GB RAM, 80 GB SSD)
- [ ] Set hostname: `hostnamectl set-hostname themisto-gw-01`
- [ ] Update system: `apt update && apt upgrade -y && reboot`
- [ ] Enable unattended-upgrades for security patches
- [ ] Create `deploy` user, configure SSH keys, disable root login

### Firewall & Network

- [ ] Configure UFW rules (Section 2)
- [ ] Verify denied ports with `nmap` from external host
- [ ] Disable IPv6 if not used: `sysctl net.ipv6.conf.all.disable_ipv6=1`

### Docker

- [ ] Install Docker Engine (official repo, not snap)
- [ ] Install Docker Compose v2 plugin
- [ ] Configure Docker daemon: log rotation, live-restore
  ```json
  // /etc/docker/daemon.json
  {
    "log-driver": "json-file",
    "log-opts": { "max-size": "50m", "max-file": "5" },
    "live-restore": true
  }
  ```
- [ ] Add `deploy` to `docker` group

### Certificates

- [ ] Generate CA key pair (Section 8)
- [ ] Generate server cert for gateway
- [ ] Set file permissions (ca.key: 0400 root:root)
- [ ] Back up CA key (Section 8.2)

### Application

- [ ] Place `docker-compose.yml` and env files in `/opt/themisto/`
- [ ] Pull images: `docker compose pull`
- [ ] Start stack: `docker compose up -d`
- [ ] Verify all containers healthy: `docker compose ps`
- [ ] Test gateway mTLS handshake from agent

### Backup & Monitoring

- [ ] Set up PostgreSQL backup cron (Section 7)
- [ ] Test backup + restore cycle
- [ ] Install node_exporter (Section 9.1)
- [ ] Set up disk usage alerts (Section 9.3)
- [ ] Configure external uptime monitor (Section 9.4)
- [ ] Verify fail2ban is active: `fail2ban-client status sshd`

### Ongoing

- [ ] Monthly: verify backup restore
- [ ] Monthly: review `auth.log` for anomalies
- [ ] Quarterly: rotate server certificates
- [ ] Annually: review CA key backup integrity
