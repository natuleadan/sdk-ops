#!/bin/sh
# libsql-dockerized restore-s3 — download backup from S3 and restore
# Uses mc (MinIO client) inside Docker — no host tools needed.
set -e

BACKUP_DIR="${BACKUP_DIR:-./backups}"
S3_BUCKET="${S3_BUCKET:-libsql-backups}"
S3_PREFIX="${S3_PREFIX:-libsql}"
S3_ENDPOINT="${S3_ENDPOINT:-s3.us-east-005.backblazeb2.com}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
MC_ALIAS="${MC_ALIAS:-s3host}"
PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"
COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"
RESTORE_FILE=""
YES=false

SQL() { docker exec "$PRIMARY_CONTAINER" curl -sf -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
HC()  { docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }

usage() {
  echo "Usage: restore-s3.sh [--yes] [backup-name]"
  echo ""
  echo "If no backup-name given, restores the latest from S3."
  echo ""
  echo "Options:"
  echo "  --yes    Skip confirmation prompt"
  echo ""
  echo "Examples:"
  echo "  restore-s3.sh                              # latest from S3"
  echo "  restore-s3.sh libsql-2026-08-21-040000.tar.gz  # specific backup"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_FILE="$1"; shift ;;
  esac
done

if [ -z "$S3_ACCESS_KEY" ] || [ -z "$S3_SECRET_KEY" ]; then
  echo "ERROR: S3_ACCESS_KEY and S3_SECRET_KEY must be set"
  exit 1
fi

echo "=== libsql-dockerized restore-s3 ==="

echo "--- Step 1: List/find backup ---"
if [ -z "$RESTORE_FILE" ]; then
  echo "  Finding latest backup in S3..."
  RESTORE_FILE=$(docker run --rm --entrypoint sh \
    minio/mc:latest -c "
mc alias set $MC_ALIAS https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 &&
mc ls $MC_ALIAS/$S3_BUCKET/$S3_PREFIX/ --json 2>/dev/null | \
  grep -o '\"key\":\"[^\"]*\.tar\.gz\"' | sed 's/\"key\":\"//;s/\"$//' | \
  sort -r | head -1 | xargs -r basename
" 2>&1 | grep "libsql-" | tail -1)

  if [ -z "$RESTORE_FILE" ]; then
    echo "  ERROR: no backups found in s3://$S3_BUCKET/$S3_PREFIX/"
    exit 1
  fi
  echo "  Latest: $RESTORE_FILE"
fi

echo "--- Step 2: Download from S3 ---"
mkdir -p "$BACKUP_DIR"
docker run --rm --entrypoint sh \
  -v "$(realpath "$BACKUP_DIR"):/backup" \
  minio/mc:latest -c "
mc alias set $MC_ALIAS https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 &&
mc cp $MC_ALIAS/$S3_BUCKET/$S3_PREFIX/$RESTORE_FILE /backup/$RESTORE_FILE &&
echo '  Downloaded: /backup/$RESTORE_FILE'
" 2>&1 | grep -v "^Alias"

LOCAL_DB="$BACKUP_DIR/$RESTORE_FILE"
if [ ! -f "$LOCAL_DB" ]; then
  echo "  ERROR: download failed"
  exit 1
fi
echo "  Size: $(du -h "$LOCAL_DB" | cut -f1)"

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop sqld and replace the database."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "--- Step 3: Stop sqld ---"
# Stop ALL project containers — after a failover the sqld ones were recreated
# by the controller via docker run, so `docker compose down` alone would leave
# them running and holding the volume.
docker ps -aq --filter "label=com.docker.compose.project=libsql-dockerized" 2>/dev/null | xargs -r docker stop 2>/dev/null
docker ps -aq --filter "label=com.docker.compose.project=libsql-dockerized" 2>/dev/null | xargs -r docker rm 2>/dev/null

echo "--- Step 4: Restore database (data.sqld full dir) ---"
docker run --rm --entrypoint sh \
  -v "libsql-dockerized_primary_data:/var/lib/sqld" \
  -v "$(realpath "$BACKUP_DIR"):/backup:ro" \
  alpine sh -c "
rm -rf /var/lib/sqld/data.sqld
mkdir -p /var/lib/sqld
tar xzf /backup/$RESTORE_FILE -C /var/lib/sqld
# sqld runs as UID 666 in the image — the tar restores root-owned files and
# sqld would open the DB readonly. Re-own everything so writes work.
chown -R 666:666 /var/lib/sqld/data.sqld
" 2>/dev/null
echo "  Database restored (full data.sqld snapshot, re-owned for sqld)"

echo "--- Step 5: Start sqld ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d 2>&1 | tail -1

echo -n "Waiting for sqld..."
until HC; do sleep 2; done
echo " OK"

RESULT=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM sqlite_master"]}')
echo "  Tables: $(echo "$RESULT" | grep -o '"cnt":[0-9]*' | cut -d: -f2 || echo '?')"

echo ""
echo "=== restore-s3 complete ==="
echo "  Run 'bash validate.sh' for full health check"
