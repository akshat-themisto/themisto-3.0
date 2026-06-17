#!/usr/bin/env bash
set -euo pipefail

CERTS_DIR="${1:-./certs}"
CA_DIR="$CERTS_DIR/ca"
VALIDITY_DAYS=3650
COUNTRY="US"
ORG="Themisto"

mkdir -p "$CA_DIR"

echo "==> Generating Root CA (ECDSA P-384, ${VALIDITY_DAYS} days)..."
openssl ecparam -genkey -name secp384r1 -noout -out "$CA_DIR/root-ca.key"
openssl req -new -x509 -sha384 \
    -key "$CA_DIR/root-ca.key" \
    -days "$VALIDITY_DAYS" \
    -subj "/C=$COUNTRY/O=$ORG/CN=Themisto Root CA" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:1" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -out "$CA_DIR/root-ca.crt"

echo "==> Generating Issuing CA (ECDSA P-256, 1095 days)..."
openssl ecparam -genkey -name prime256v1 -noout -out "$CA_DIR/issuing-ca.key"
openssl req -new -sha256 \
    -key "$CA_DIR/issuing-ca.key" \
    -subj "/C=$COUNTRY/O=$ORG/CN=Themisto Issuing CA" \
    -out "$CA_DIR/issuing-ca.csr"

openssl x509 -req -sha384 \
    -in "$CA_DIR/issuing-ca.csr" \
    -CA "$CA_DIR/root-ca.crt" \
    -CAkey "$CA_DIR/root-ca.key" \
    -CAcreateserial \
    -days 1095 \
    -extfile <(printf "basicConstraints=critical,CA:TRUE,pathlen:0\nkeyUsage=critical,keyCertSign,cRLSign") \
    -out "$CA_DIR/issuing-ca.crt"

rm -f "$CA_DIR/issuing-ca.csr" "$CA_DIR/root-ca.srl"

cat "$CA_DIR/issuing-ca.crt" "$CA_DIR/root-ca.crt" > "$CA_DIR/ca-chain.pem"

chmod 0400 "$CA_DIR/root-ca.key" "$CA_DIR/issuing-ca.key"
chmod 0444 "$CA_DIR/root-ca.crt" "$CA_DIR/issuing-ca.crt" "$CA_DIR/ca-chain.pem"

echo "==> CA files generated in $CA_DIR"
echo "    root-ca.key      (KEEP OFFLINE — back up securely)"
echo "    root-ca.crt"
echo "    issuing-ca.key"
echo "    issuing-ca.crt"
echo "    ca-chain.pem     (issuing + root, for TLS client CA pool)"
