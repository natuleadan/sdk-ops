#!/bin/sh
# libsql-dockerized backup — copy .db + WAL snapshots
set -e

PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATE=$(date +%F-%H%M%S)

echo "=== libsql-dockerized backup ==="

docker inspect "$PRIMARY_CONTAINER" >/dev/null 2>&1 || {
  echo "ERROR: container $PRIMARY_CONTAINER not found"
  exit 1
}

mkdir -p "$BACKUP_DIR"

DATA_FILE=$(docker exec "$PRIMARY_CONTAINER" sh -c 'find /var/lib/sqld -path "*/dbs/default/data" -type f 2>/dev/null | head -1')
if [ -z "$DATA_FILE" ]; then
  echo "  WARN: database file not found"
else
  docker cp "$PRIMARY_CONTAINER:$DATA_FILE" "$BACKUP_DIR/libsql-$DATE.db" 2>/dev/null
  echo "  Local: $BACKUP_DIR/libsql-$DATE.db"
fi

echo ""
echo "=== backup complete ==="
echo "  Restore: bash restore.sh $BACKUP_DIR/libsql-$DATE.db"
