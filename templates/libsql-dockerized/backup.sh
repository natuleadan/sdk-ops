#!/bin/sh
# libsql-dockerized backup — copy data.sqld (tar.gz)
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

echo "--- Step 1: Consistent snapshot (tar of data.sqld) ---"
# sqld keeps its state in data.sqld/ (WAL frames + wallog), not a plain .db.
DATA_DIR="/var/lib/sqld/data.sqld"
BACKUP_FILE="$BACKUP_DIR/libsql-$DATE.tar.gz"

if ! docker exec "$PRIMARY_CONTAINER" sh -c "test -d $DATA_DIR" 2>/dev/null; then
  echo "  WARN: data dir not found"
else
  docker exec "$PRIMARY_CONTAINER" sh -c "cd /var/lib/sqld && tar czf - data.sqld" > "$BACKUP_FILE" 2>/dev/null
  echo "  Local: $BACKUP_FILE ($(du -h "$BACKUP_FILE" | cut -f1))"
fi

echo ""
echo "=== backup complete ==="
echo "  Restore: bash restore.sh $BACKUP_FILE"
