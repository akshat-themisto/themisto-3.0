#!/usr/bin/env bash
set -euo pipefail

# Themisto VPS Deployment Script
# Run on a fresh Ubuntu 24.04 LTS VPS.
# Prerequisites: SSH access as a sudo-capable user.
#
# Usage:
#   scp -r . user@vps:/opt/themisto
#   ssh user@vps 'cd /opt/themisto && sudo PUBLIC_BACKEND_URL=https://control.example.com PUBLIC_GATEWAY_URL=https://gateway.example.com bash scripts/deploy-vps.sh'

DEPLOY_DIR="/opt/themisto"
cd "$DEPLOY_DIR" 2>/dev/null || { echo "Run from $DEPLOY_DIR"; exit 1; }

PUBLIC_BACKEND_URL="${PUBLIC_BACKEND_URL:-}"
PUBLIC_GATEWAY_URL="${PUBLIC_GATEWAY_URL:-}"
if [[ -z "$PUBLIC_BACKEND_URL" || -z "$PUBLIC_GATEWAY_URL" ]]; then
  cat >&2 <<'ERR'
error: PUBLIC_BACKEND_URL and PUBLIC_GATEWAY_URL must be set for production deployment.

Example:
  sudo PUBLIC_BACKEND_URL=https://control.example.com \
       PUBLIC_GATEWAY_URL=https://gateway.example.com \
       bash scripts/deploy-vps.sh
ERR
  exit 1
fi

upsert_env() {
  local key="$1"
  local value="$2"
  if grep -q "^${key}=" .env; then
    sed -i "s#^${key}=.*#${key}=${value}#" .env
  else
    echo "${key}=${value}" >> .env
  fi
}

echo "=== Themisto VPS Deployment ==="

# ── 1. System hardening ──────────────────────────────────────────────────────
echo "[1/7] Hardening system..."

apt-get update -qq && apt-get upgrade -y -qq

apt-get install -y -qq ufw fail2ban curl wget

ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp comment "SSH"
ufw allow 443/tcp comment "Gateway mTLS"
ufw allow 8443/tcp comment "Backend enrollment/API"
ufw --force enable

if ! grep -q "^PermitRootLogin no" /etc/ssh/sshd_config; then
    sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
    sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
    systemctl restart ssh || systemctl restart sshd
fi

systemctl enable fail2ban
systemctl start fail2ban

# ── 2. Docker ────────────────────────────────────────────────────────────────
echo "[2/7] Installing Docker..."

if ! command -v docker &>/dev/null; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker
    systemctl start docker
fi

# ── 3. Generate certificates ────────────────────────────────────────────────
echo "[3/7] Generating certificates..."

if [ ! -f certs/ca/ca-chain.pem ]; then
    bash scripts/gen-ca.sh
    bash scripts/gen-server-cert.sh
    echo "  Certificates generated in certs/"
else
    echo "  Certificates already exist, skipping"
fi

# Keys must be readable inside Docker containers
chmod 0644 certs/ca/issuing-ca.key certs/server/server.key 2>/dev/null || true

# ── 4. Environment file ─────────────────────────────────────────────────────
echo "[4/7] Setting up environment..."

if [ ! -f .env ]; then
    POSTGRES_PASSWORD=$(openssl rand -hex 24)
    ADMIN_API_KEY=$(openssl rand -hex 32)
    cat > .env <<ENVEOF
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
ADMIN_API_KEY=$ADMIN_API_KEY
PUBLIC_BACKEND_URL=$PUBLIC_BACKEND_URL
PUBLIC_GATEWAY_URL=$PUBLIC_GATEWAY_URL
ENVEOF
    chmod 600 .env
    echo "  .env created (save ADMIN_API_KEY: $ADMIN_API_KEY)"
else
    upsert_env "PUBLIC_BACKEND_URL" "$PUBLIC_BACKEND_URL"
    upsert_env "PUBLIC_GATEWAY_URL" "$PUBLIC_GATEWAY_URL"
    chmod 600 .env
    echo "  .env already exists, updated PUBLIC_* URLs"
fi

# ── 5. Deploy ────────────────────────────────────────────────────────────────
echo "[5/7] Starting services..."

docker compose pull postgres 2>/dev/null || true
docker compose up -d --build

echo "  Waiting for services to become healthy..."
sleep 10
docker compose ps

# ── 6. Backup cron ───────────────────────────────────────────────────────────
echo "[6/7] Setting up backup cron..."

BACKUP_SCRIPT="$DEPLOY_DIR/scripts/pg_backup.sh"
chmod +x "$BACKUP_SCRIPT" 2>/dev/null || true
CRON_LINE="0 3 * * * $BACKUP_SCRIPT"
(crontab -l 2>/dev/null | grep -qF "$BACKUP_SCRIPT") || \
    (crontab -l 2>/dev/null; echo "$CRON_LINE") | crontab -
echo "  Daily backup cron set for 03:00"

# ── 7. Verification ─────────────────────────────────────────────────────────
echo "[7/7] Verifying deployment..."

echo -n "  Gateway health: "
curl -sf http://localhost:8080/healthz && echo " OK" || echo " FAILED"

echo -n "  Backend health: "
curl -sf http://localhost:8443/healthz && echo " OK" || echo " FAILED"

echo ""
echo "=== Deployment complete ==="
echo ""
echo "Next steps:"
echo "  1. Enroll devices: bash scripts/enroll-device.sh <device-name> <os>"
echo "  2. Monitor: docker compose logs -f"
echo "  3. Metrics: curl http://localhost:8080/metrics"
echo "  4. Backup CA keys to offline storage"
