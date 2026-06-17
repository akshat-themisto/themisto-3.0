#!/usr/bin/env bash
set -euo pipefail

CERTS_DIR="${1:-./certs}"
CA_DIR="$CERTS_DIR/ca"
SERVER_DIR="$CERTS_DIR/server"
HOSTNAME="${2:-localhost}"
EXTRA_IP="${3:-}"   # Optional: VPS public IP so agents can verify (e.g. 165.232.186.51)
VALIDITY_DAYS=365

mkdir -p "$SERVER_DIR"

if [ ! -f "$CA_DIR/issuing-ca.key" ] || [ ! -f "$CA_DIR/issuing-ca.crt" ]; then
    echo "ERROR: Issuing CA not found in $CA_DIR. Run gen-ca.sh first."
    exit 1
fi

echo "==> Generating server certificate for: $HOSTNAME"
openssl ecparam -genkey -name prime256v1 -noout -out "$SERVER_DIR/server.key"

openssl req -new -sha256 \
    -key "$SERVER_DIR/server.key" \
    -subj "/O=Themisto/CN=$HOSTNAME" \
    -out "$SERVER_DIR/server.csr"

# Build SAN: always localhost + 127.0.0.1; add HOSTNAME if not localhost; add EXTRA_IP if set
SAN="DNS:localhost,IP:127.0.0.1"
[ "$HOSTNAME" != "localhost" ] && SAN="DNS:$HOSTNAME,$SAN"
[ -n "$EXTRA_IP" ] && SAN="$SAN,IP:$EXTRA_IP"

openssl x509 -req -sha256 \
    -in "$SERVER_DIR/server.csr" \
    -CA "$CA_DIR/issuing-ca.crt" \
    -CAkey "$CA_DIR/issuing-ca.key" \
    -CAcreateserial \
    -days "$VALIDITY_DAYS" \
    -extfile <(printf "subjectAltName=%s\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth" "$SAN") \
    -out "$SERVER_DIR/server.crt"

rm -f "$SERVER_DIR/server.csr" "$CA_DIR/issuing-ca.srl"

chmod 0400 "$SERVER_DIR/server.key"
chmod 0444 "$SERVER_DIR/server.crt"

echo "==> Server certificate generated in $SERVER_DIR"
echo "    server.key"
echo "    server.crt  (valid for $VALIDITY_DAYS days, SAN: $SAN)"

echo "==> Verifying chain..."
openssl verify -CAfile "$CA_DIR/ca-chain.pem" "$SERVER_DIR/server.crt"
