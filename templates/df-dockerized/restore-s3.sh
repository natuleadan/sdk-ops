#!/bin/sh
# df-dockerized restore-s3 — download backup from S3 and restore
# Uses s3cmd on the host (pre-installed, ~/.s3cfg). External S3 only.
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
S3_BUCKET="${S3_BUCKET:-df-backups}"
S3_PREFIX="${S3_PREFIX:-df}"
PRIMARY_CONTAINER="df-dockerized-dragonfly-primary-1"
COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"
RESTORE_FILE=""
YES=false

RC() { docker exec "$PRIMARY_CONTAINER" redis-cli -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

usage() {
  echo "Usage: restore-s3.sh [--yes] [backup-name]"
  echo ""
  echo "If no backup-name given, restores the latest from S3."
  echo ""
  echo "Options:"
  echo "  --yes    Skip confirmation prompt"
  echo ""
  echo "Examples:"
  echo "  restore-s3.sh                          # latest from S3"
  echo "  restore-s3.sh df-2026-08-27-040000.tar.gz  # specific backup"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_FILE="$1"; shift ;;
  esac
done

command -v s3cmd >/dev/null 2>&1 || {
  echo "ERROR: s3cmd not installed (apt-get install -y s3cmd)"
  exit 1
}

echo "=== df-dockerized restore-s3 ==="

echo "--- Step 1: Find backup ---"
if [ -z "$RESTORE_FILE" ]; then
  echo "  Finding latest backup in S3..."
  RESTORE_FILE=$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | grep '\.tar\.gz' | awk '{print $4}' | sort -r | head -1 | xargs -r basename)

  if [ -z "$RESTORE_FILE" ]; then
    echo "  ERROR: no backups found in s3://$S3_BUCKET/$S3_PREFIX/"
    exit 1
  fi
  echo "  Latest: $RESTORE_FILE"
fi

echo "--- Step 2: Download from S3 ---"
mkdir -p "$BACKUP_DIR"
s3cmd get --force "s3://$S3_BUCKET/$S3_PREFIX/$RESTORE_FILE" "$BACKUP_DIR/$RESTORE_FILE" 2>&1

LOCAL_BACKUP="$BACKUP_DIR/$RESTORE_FILE"
if [ ! -f "$LOCAL_BACKUP" ]; then
  echo "  ERROR: download failed"
  exit 1
fi
echo "  Size: $(du -h "$LOCAL_BACKUP" | cut -f1)"

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop Dragonfly and replace all data."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "--- Step 3: Stop ALL project containers ---"
docker ps -aq --filter "label=com.docker.compose.project=df-dockerized" 2>/dev/null | xargs -r docker stop 2>/dev/null
docker ps -aq --filter "label=com.docker.compose.project=df-dockerized" 2>/dev/null | xargs -r docker rm 2>/dev/null

echo "--- Step 4: Restore snapshot into volumes ---"
docker run --rm \
  -v "df-dockerized_primary_data:/data" \
  -v "df-dockerized_replica_data:/data-replica" \
  -v "df-dockerized_replica2_data:/data-replica2" \
  -v "$(realpath "$BACKUP_DIR"):/backup:ro" \
  alpine sh -c "rm -rf /data/* /data-replica/* /data-replica2/* 2>/dev/null; tar xzf /backup/$RESTORE_FILE -C /data; chmod 644 /data/*.dfs 2>/dev/null"
echo "  Volumes restored (primary gets the snapshot, replicas resync on start)"

echo "--- Step 5: Start services ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d 2>&1 | tail -1

echo -n "Waiting for Dragonfly..."
i=0
while [ "$i" -lt 60 ]; do
  RC PING 2>/dev/null | grep -q "PONG" && break
  i=$((i + 1))
  sleep 2
done
RC PING 2>/dev/null | grep -q "PONG" || {
  echo " FAIL"
  echo "ERROR: Dragonfly did not come back after restore"
  exit 1
}
echo " OK"

cd "$COMPOSE_DIR" && bash init.sh 2>&1 | tail -3

KEYS=$(RC DBSIZE)
echo "  Keys restored: ${KEYS:-0}"

echo ""
echo "=== restore-s3 complete ==="
echo "  Run 'bash validate.sh' for full health check"
