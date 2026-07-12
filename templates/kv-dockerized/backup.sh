#!/bin/sh
# kv-dockerized backup — trigger BGSAVE, copy snapshot to local or S3 storage
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATE=$(date +%F-%H%M%S)
PRIMARY_CONTAINER="kv-dockerized-dragonfly-primary-1"
REPLICA_CONTAINER="kv-dockerized-dragonfly-replica-1"

RC()     { docker exec "$PRIMARY_CONTAINER" redis-cli -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP() { docker exec "$REPLICA_CONTAINER" redis-cli -p 6380 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== kv-dockerized backup ==="

echo "Triggering BGSAVE..."
RC BGSAVE
echo -n "Waiting for snapshot..."
for i in $(seq 1 30); do
  INPROGRESS=$(RC INFO PERSISTENCE | grep "rdb_bgsave_in_progress" | cut -d: -f2 | tr -d '\r\n')
  [ "$INPROGRESS" = "0" ] || [ -z "$INPROGRESS" ] && break
  sleep 1
done
echo " done"

mkdir -p "$BACKUP_DIR"
TIMESTAMP=$(docker exec "$PRIMARY_CONTAINER" sh -c 'ls /data/dump-*summary.dfs 2>/dev/null | sed "s/.*dump-//;s/-summary.*//" | sort | tail -1')
if [ -n "$TIMESTAMP" ]; then
  mkdir -p "$BACKUP_DIR/$DATE"
  for f in $(docker exec "$PRIMARY_CONTAINER" sh -c "ls /data/dump-$TIMESTAMP-*.dfs 2>/dev/null"); do
    docker cp "$PRIMARY_CONTAINER:$f" "$BACKUP_DIR/$DATE/" 2>/dev/null
  done
  LOCAL_FILE="$BACKUP_DIR/$DATE"
  echo "  Local: $LOCAL_FILE ($(du -sh "$LOCAL_FILE" | cut -f1))"
fi

# Upload to S3 if configured
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$LOCAL_FILE" ]; then
  MINIO_CONTAINER=$(docker ps --format '{{.Names}}' | grep minio | head -1)
  if [ -n "$MINIO_CONTAINER" ]; then
    tar czf - -C "$LOCAL_FILE" . | docker exec -i "$MINIO_CONTAINER" sh -c "
      mc alias set s3host '$S3_ENDPOINT' '${S3_KEY:-}' '${S3_SECRET:-}' 2>/dev/null
      mc pipe s3host/$S3_BUCKET/$DATE.tar.gz
    " 2>&1
    echo "  S3: $S3_BUCKET/$DATE.tar.gz"
  fi
fi

# Replica BGSAVE
RC_REP BGSAVE
echo "  Replica BGSAVE triggered"

echo ""
echo "=== backup complete ==="
echo "  Restore: bash restore.sh $LOCAL_FILE"
