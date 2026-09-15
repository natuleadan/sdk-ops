#!/bin/sh
# pgsql-docker restore — pgbackrest restore with PITR support
set -e

SVC_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SVC_DIR/.env" ]; then set -a; . "$SVC_DIR/.env"; set +a; fi

CONTAINER="${CONTAINER:-pgsql-docker-postgres-1}"
PG_USER="${PG_USER:-dev}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
STANZA="${STANZA:-main}"
COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"

MODE="latest"
TARGET=""
TYPE="time"
SET=""
YES=false
DELTA=false

usage() {
  echo "Usage: restore.sh [OPTIONS]"
  echo "Restore PostgreSQL from pgbackrest backup"
  echo ""
  echo "Options:"
  echo "  --mode latest|full|pitr    Restore mode (default: latest)"
  echo "  --target 'YYYY-MM-DD HH:MM:SS'  PITR target timestamp"
  echo "  --type time|xid|name|immediate   PITR target type (default: time)"
  echo "  --stanza NAME              Stanza name (default: main)"
  echo "  --set BACKUP_LABEL         Restore specific backup by label"
  echo "  --delta                    Delta restore (only changed files)"
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
    --set) SET="$2"; shift 2 ;;
    --delta) DELTA=true; shift ;;
    --yes) YES=true; shift ;;
    --help|-h) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

echo "=== pgsql-docker restore ==="
echo "Stanza: $STANZA  Mode: $MODE  Delta: $DELTA"

# Build pgbackrest restore args
ARGS="--stanza=$STANZA --db-path=/var/lib/postgresql/18/docker"
if [ -n "$SET" ]; then
  ARGS="$ARGS --set=$SET"
fi
if [ "$DELTA" = true ]; then
  ARGS="$ARGS --delta"
fi

case "$MODE" in
  latest)
    # --type=immediate: recover to the backup consistency point and promote
    # (new timeline, no WAL replay past it). This pins data at the backup
    # moment AND keeps the archive consistent (no resetwal, no diverged
    # segments wedging the archiver).
    ARGS="--stanza=$STANZA --db-path=/var/lib/postgresql/18/docker --type=immediate"
    echo "Target: latest backup (recover to consistency, then promote)"
    ;;
  full)
    BACKUP_SET=$(docker exec -e PGPASSWORD="$PG_PASSWORD" $CONTAINER pgbackrest --stanza=$STANZA info 2>/dev/null | grep -oE 'full backup: [0-9T\-]+' | tail -1 | cut -d' ' -f3)
    ARGS="$ARGS --set=$BACKUP_SET --type=immediate"
    echo "Target: full backup $BACKUP_SET (recover to consistency, then promote)"
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

# Run restore with the same postgres image the service uses (it carries
# pgbackrest) and the stopped primary's volumes (data + pgbackrest.conf).
# A named helper image does not exist - a previous revision tried
# `docker run pgsql-docker:latest` here, which always failed and then
# reported success on an empty datadir.
echo "Running pgbackrest restore..."
IMG="$(docker inspect "$CONTAINER" --format '{{"{{"}}.Config.Image{{"}}"}}')"
rc=0
docker run --rm --entrypoint sh --volumes-from "$CONTAINER" "$IMG" \
  -c "
pgbackrest $ARGS restore 2>&1
# The helper runs as root: hand the restored files back to postgres.
chown -R postgres:postgres /var/lib/postgresql/18/docker 2>/dev/null || true
" > /tmp/pg-restore-out.log 2>&1 || rc=$?
tail -3 /tmp/pg-restore-out.log
if [ "$rc" -ne 0 ]; then echo "FAIL: pgbackrest restore failed (see above)"; exit 1; fi
# Recovery (backup_label/recovery.signal) is left intact on purpose: postgres
# replays to the restore target and promotes on a NEW timeline, so the archive
# never diverges (a resetwal hack here recycles segment numbers and wedges the
# archiver on checksum collisions).

# Start PostgreSQL
echo "Starting PostgreSQL..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" start postgres 2>&1 | tail -1
tries=0
until docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pg_isready -U "$PG_USER" -d "$PG_DATABASE" -h localhost 2>/dev/null; do
  tries=$((tries + 1))
  if [ "$tries" -ge 60 ]; then echo "FAIL: postgres never came back after restore"; exit 1; fi
  sleep 2
done

# --type=immediate recovers to consistency and PAUSES: resume to promote,
# otherwise the primary stays read-only and the stanza check goes red.
echo "Resuming recovery (promote)..."
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -tAc \
  "SELECT pg_wal_replay_resume();" 2>&1 | tail -1
tries=0
until docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -tAc \
  "SELECT pg_is_in_recovery();" 2>/dev/null | grep -q "^f$"; do
  tries=$((tries + 1))
  if [ "$tries" -ge 30 ]; then echo "FAIL: postgres never promoted (still in recovery)"; exit 1; fi
  sleep 2
done
echo "  Promoted (writable primary)"

# The wipe dropped the replication slots: recreate them before replicas start.
for SLOT in rep1 rep2; do
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -tAc \
    "SELECT pg_create_physical_replication_slot('$SLOT') WHERE NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = '$SLOT')" 2>&1 | tail -1
done
echo "  Replication slots: rep1 rep2 (recreate any standby with a wiped PGDATA + restart so it reclones)"

echo ""
echo "=== restore complete ==="
echo "  Verify: PGPASSWORD=<redacted> psql -h localhost -U $PG_USER -d $PG_DATABASE -c 'SELECT count(*) FROM <table>;'"
echo "  NOTE: standbys keep newer WAL and cannot stream backwards - wipe their"
echo "  PGDATA (data/pg-replica*, container stopped) and restart them to reclone."
