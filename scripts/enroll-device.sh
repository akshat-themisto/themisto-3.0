#!/usr/bin/env bash
set -euo pipefail

BACKEND_URL="${BACKEND_URL:-http://localhost:8443}"
API_KEY="${ADMIN_API_KEY:-themisto-dev-key-change-me}"
CERTS_DIR="${CERTS_DIR:-./certs}"
CA_CHAIN="$CERTS_DIR/ca/ca-chain.pem"
DEVICE_NAME="${1:?Usage: enroll-device.sh <device-name> [os]}"
DEVICE_OS="${2:-darwin}"
ORG_ID="a0000000-0000-0000-0000-000000000001"
OUT_DIR="$CERTS_DIR/devices/$DEVICE_NAME"

mkdir -p "$OUT_DIR"

echo "==> Step 1: Register device '$DEVICE_NAME'..."
REGISTER_RESP=$(curl -s -X POST "$BACKEND_URL/api/v1/devices" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"org_id\": \"$ORG_ID\", \"device_name\": \"$DEVICE_NAME\", \"os\": \"$DEVICE_OS\", \"agent_version\": \"0.1.0\"}")

echo "$REGISTER_RESP" | python3 -m json.tool 2>/dev/null || echo "$REGISTER_RESP"

DEVICE_ID=$(echo "$REGISTER_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['device_id'])")
TOKEN=$(echo "$REGISTER_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['enrollment_token'])")

echo "    Device ID: $DEVICE_ID"
echo "    Token:     $TOKEN"

echo "==> Step 2: Generate device key pair (ECDSA P-256)..."
openssl ecparam -genkey -name prime256v1 -noout -out "$OUT_DIR/device.key"

echo "==> Step 3: Create CSR..."
openssl req -new -sha256 \
    -key "$OUT_DIR/device.key" \
    -subj "/O=Themisto Dev Org/CN=$DEVICE_ID" \
    -out "$OUT_DIR/device.csr"

echo "==> Step 4: Submit CSR..."
CSR_RESP=$(curl -s -X POST "$BACKEND_URL/api/v1/devices/$DEVICE_ID/csr" \
    -H "X-Enrollment-Token: $TOKEN" \
    -H "Content-Type: application/pkcs10" \
    --data-binary @"$OUT_DIR/device.csr")

echo "$CSR_RESP" | python3 -m json.tool 2>/dev/null || echo "$CSR_RESP"

echo "$CSR_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['certificate'])" > "$OUT_DIR/device.crt"
echo "$CSR_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['ca_chain'])" > "$OUT_DIR/ca-chain.pem"

rm -f "$OUT_DIR/device.csr"
chmod 0400 "$OUT_DIR/device.key"

echo ""
echo "==> Device enrolled successfully!"
echo "    $OUT_DIR/device.key   — private key"
echo "    $OUT_DIR/device.crt   — signed certificate"
echo "    $OUT_DIR/ca-chain.pem — CA chain for verification"
echo ""
echo "Test mTLS connection:"
echo "    curl --cert $OUT_DIR/device.crt --key $OUT_DIR/device.key --cacert $CA_CHAIN https://localhost/healthz"
