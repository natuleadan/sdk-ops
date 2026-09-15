#!/bin/sh
# df-dockerized backup-s3 — consistent snapshot (BGSAVE) + upload to S3 + retention
# Uses s3cmd on the host (pre-installed, ~/.s3cfg). External S3 only.
set -e

SVC_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SVC_DIR/.env" ]; then . "$SVC_DIR/.env"; fi

PRIMARY_CONTAINER="df-dockerized-dragonfly-primary-1"
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATE=$(date +%F-%H%M%S)
S3_BUCKET="${S3_BUCKET:-df-backups}"
S3_PREFIX="${S3_PREFIX:-df}"
RETENTION="${RETENTION:-7}"

RC() { docker exec "$PRIMARY_CONTAINER" redis-cli -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== df-dockerized backup-s3 ==="

docker inspect "$PRIMARY_CONTAINER" >/dev/null 2>&1 || {
  echo "ERROR: container $PRIMARY_CONTAINER not found"
  exit 1
}

command -v s3cmd >/dev/null 2>&1 || {
  echo "ERROR: s3cmd not installed (apt-get install -y s3cmd)"
  exit 1
}

mkdir -p "$BACKUP_DIR"

echo "--- Step 1: Consistent snapshot (BGSAVE) ---"
RC BGSAVE
echo -n "  Waiting for snapshot..."
i=0
while [ "$i" -lt 30 ]; do
  INPROGRESS=$(RC INFO PERSISTENCE | grep "rdb_bgsave_in_progress" | cut -d: -f2 | tr -d '\r\n')
  [ "$INPROGRESS" = "0" ] && break
  i=$((i + 1))
  sleep 1
done
echo " done"

# Dragonfly persists .dfs snapshot files in /data — copy the latest set out.
TIMESTAMP=$(docker exec "$PRIMARY_CONTAINER" sh -c 'ls /data/dump-*summary.dfs 2>/dev/null | sed "s/.*dump-//;s/-summary.*//" | sort | tail -1')
if [ -z "$TIMESTAMP" ]; then
  echo "ERROR: no snapshot files found in /data (BGSAVE failed?)"
  exit 1
fi

LOCAL_BACKUP="$BACKUP_DIR/df-$DATE.tar.gz"
docker exec "$PRIMARY_CONTAINER" sh -c "cd /data && tar czf - dump-$TIMESTAMP-*.dfs" > "$LOCAL_BACKUP" 2>/dev/null
echo "  Local: $LOCAL_BACKUP ($(du -h "$LOCAL_BACKUP" | cut -f1))"

echo "--- Step 2: Upload to S3 ---"
s3cmd put "$LOCAL_BACKUP" "s3://$S3_BUCKET/$S3_PREFIX/df-$DATE.tar.gz" 2>&1
echo "  Uploaded: s3://$S3_BUCKET/$S3_PREFIX/df-$DATE.tar.gz"

echo "--- Step 3: Retention (keep last $RETENTION) ---"
EXISTING=$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | grep '\.tar\.gz' | awk '{print $4}' | sort -r)
COUNT=$(echo "$EXISTING" | grep -c . || true)
if [ "$COUNT" -gt "$RETENTION" ]; then
  echo "$EXISTING" | tail -n +$((RETENTION + 1)) | while read -r key; do
    [ -n "$key" ] && echo "  Removing old: $key" && s3cmd del "$key" 2>/dev/null || true
  done
fi

rm -f "$LOCAL_BACKUP"

echo ""
echo "=== backup-s3 complete ==="
echo "  S3: s3://$S3_BUCKET/$S3_PREFIX/df-$DATE.tar.gz"
echo "  Restore: bash restore-s3.sh"
