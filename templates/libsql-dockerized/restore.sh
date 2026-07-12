#!/bin/sh
# libsql-dockerized restore — restore libSQL database from backup
set -e

RESTORE_FILE=""
YES=false
COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"
PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"

SQL() { docker exec "$PRIMARY_CONTAINER" curl -sf -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
HC()  { docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }

usage() {
  echo "Usage: restore.sh [--yes] <backup-file.db>"
  echo ""
  echo "Options:"
  echo "  --yes    Skip confirmation prompt"
  echo ""
  echo "Examples:"
  echo "  restore.sh backups/libsql-2026-07-12-040000.db"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_FILE="$1"; shift ;;
  esac
done

if [ -z "$RESTORE_FILE" ] || [ ! -f "$RESTORE_FILE" ]; then
  echo "Usage: restore.sh [--yes] <backup-file.db>"
  ls -lh backups/*.db 2>/dev/null || echo "  (no backups found)"
  exit 1
fi

echo "=== libsql-dockerized restore ==="
echo "  File: $RESTORE_FILE"

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop sqld and replace the database."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "Stopping sqld..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" down 2>&1 | tail -1

echo "Copying database to data volume..."
docker run --rm \
  -v "libsql-dockerized_primary_data:/data" \
  -v "$(dirname "$(realpath "$RESTORE_FILE")"):/backup:ro" \
  alpine sh -c "
mkdir -p /data/dbs/default
cp /backup/$(basename "$RESTORE_FILE") /data/dbs/default/data
chmod 644 /data/dbs/default/data
" 2>/dev/null
echo "  Database file restored"

WAL_FILE="$(dirname "$RESTORE_FILE")/$(basename "$RESTORE_FILE" .db)-wal"
if [ -f "$WAL_FILE" ]; then
  docker run --rm \
    -v "libsql-dockerized_primary_data:/data" \
    -v "$(dirname "$WAL_FILE"):/backup:ro" \
    alpine cp "/backup/$(basename "$WAL_FILE")" "/data/dbs/default/data-wal" 2>/dev/null
  echo "  WAL file restored"
fi

echo "Starting sqld..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d 2>&1 | tail -1

echo -n "Waiting for sqld..."
until HC; do sleep 2; done
echo ""

RESULT=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM sqlite_master"]}')
echo "  Tables: $(echo "$RESULT" | grep -o '"cnt":[0-9]*' | cut -d: -f2 || echo '?')"

echo ""
echo "=== restore complete ==="
echo "  Run 'bash validate.sh' for full health check"
