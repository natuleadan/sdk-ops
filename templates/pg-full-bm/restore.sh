#!/bin/sh
# pg-full-bm restore — pgbackrest restore with PITR support
set -e

CONTAINER="${CONTAINER:-pg-full-bm-postgres-1}"
PG_USER="${PG_USER:-dev}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
STANZA="${STANZA:-main}"
COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"

MODE="latest"
TARGET=""
TYPE="time"
YES=false

usage() {
  echo "Usage: restore.sh [OPTIONS]"
  echo "Restore PostgreSQL from pgbackrest backup"
  echo ""
  echo "Options:"
  echo "  --mode latest|full|pitr    Restore mode (default: latest)"
  echo "  --target 'YYYY-MM-DD HH:MM:SS'  PITR target timestamp"
  echo "  --type time|xid|name|immediate   PITR target type (default: time)"
  echo "  --stanza NAME              Stanza name (default: main)"
  echo "  --yes                      Skip confirmation prompt"
  echo "  --help                     Show this help"
  echo ""
  echo "Examples:"
  echo "  restore.sh                    # Latest full backup"
  echo "  restore.sh --mode pitr --target '2026-07-12 15:30:00' # PITR"
  echo "  restore.sh --mode full        # Restore last full backup"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    --target) TARGET="$2"; shift 2 ;;
    --type) TYPE="$2"; shift 2 ;;
    --stanza) STANZA="$2"; shift 2 ;;
    --yes) YES=true; shift ;;
    --help|-h) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

echo "=== pg-full-bm restore ==="
echo "Stanza: $STANZA  Mode: $MODE"

# Build pgbackrest restore args
ARGS="--stanza=$STANZA --db-path=/var/lib/postgresql/18/docker"

case "$MODE" in
  latest)
    ARGS="--stanza=$STANZA --db-path=/var/lib/postgresql/18/docker --type=immediate"
    echo "Target: latest backup (immediate recovery)"
    ;;
  full)
    BACKUP_SET=$(docker exec -e PGPASSWORD="$PG_PASSWORD" $CONTAINER pgbackrest --stanza=$STANZA info 2>/dev/null | grep -oE 'full backup: [0-9T\-]+' | tail -1 | cut -d' ' -f3)
    ARGS="$ARGS --set=$BACKUP_SET"
    echo "Target: full backup $BACKUP_SET"
    ;;
  pitr)
    if [ -z "$TARGET" ]; then
      echo "ERROR: --target required for pitr mode"
      exit 1
    fi
    ARGS="--stanza=$STANZA --db-path=/var/lib/postgresql/18/docker --type=$TYPE --target='$TARGET'"
    echo "Target: PITR to $TARGET ($TYPE)"
    ;;
  *)
    echo "ERROR: unknown mode $MODE (use latest, full, or pitr)"
    exit 1
    ;;
esac

# Confirm
if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop PostgreSQL and replace data directory."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

# Stop PostgreSQL
echo "Stopping PostgreSQL..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" stop postgres 2>&1 | tail -1

# Remove old data
echo "Removing old data..."
rm -rf "$COMPOSE_DIR/data/pg/18/docker"

# Run restore via temporary container with same volumes
echo "Running pgbackrest restore..."
docker run --rm \
  -v "$COMPOSE_DIR/data/pg:/var/lib/postgresql" \
  -v "$COMPOSE_DIR/data/pgbackrest:/var/lib/pgbackrest" \
  -v "$COMPOSE_DIR/pgbackrest.conf:/etc/pgbackrest/pgbackrest.conf:ro" \
  -e PGPASSWORD="$PG_PASSWORD" \
  postgres:18-alpine \
  sh -c "
apk add --no-cache pgbackrest 2>&1 | tail -1
pgbackrest $ARGS restore 2>&1
# Clean conflicting settings from restored config
rm -f /var/lib/postgresql/18/docker/postgresql.auto.conf
" 2>&1 | tail -3

# Start PostgreSQL
echo "Starting PostgreSQL..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" start postgres 2>&1 | tail -1

echo ""
echo "=== restore complete ==="
echo "  Verify: PGPASSWORD=$PG_PASSWORD psql -h localhost -U $PG_USER -d postgres -c 'SELECT now();'"
