#!/usr/bin/env bash
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
CONTAINER="${PG_CONTAINER:-themisto-postgres-1}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RETAIN_DAYS=30

mkdir -p "$BACKUP_DIR"

echo "==> Creating PostgreSQL backup..."
docker exec "$CONTAINER" pg_dump -U themisto -Fc themisto \
    > "$BACKUP_DIR/themisto_${TIMESTAMP}.dump"

DUMP_SIZE=$(du -h "$BACKUP_DIR/themisto_${TIMESTAMP}.dump" | cut -f1)
echo "    Dump size: $DUMP_SIZE"

if [ -f /etc/themisto/backup.key ]; then
    echo "==> Encrypting backup..."
    gpg --batch --yes --symmetric --cipher-algo AES256 \
        --passphrase-file /etc/themisto/backup.key \
        "$BACKUP_DIR/themisto_${TIMESTAMP}.dump"
    rm "$BACKUP_DIR/themisto_${TIMESTAMP}.dump"
    echo "    Encrypted: themisto_${TIMESTAMP}.dump.gpg"
else
    echo "    WARNING: No encryption key at /etc/themisto/backup.key — backup is unencrypted"
fi

echo "==> Pruning backups older than $RETAIN_DAYS days..."
PRUNED=$(find "$BACKUP_DIR" -name "themisto_*.dump*" -mtime +"$RETAIN_DAYS" -print -delete | wc -l)
echo "    Pruned $PRUNED old backup(s)"

echo "==> Backup complete: $BACKUP_DIR"
