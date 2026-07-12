#!/bin/sh
# pg-full-bm backup — pgbackrest full backup to local or S3 storage
set -e

CONTAINER="${CONTAINER:-pg-full-bm-postgres-1}"
PG_USER="${PG_USER:-dev}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
STANZA="${STANZA:-main}"
TYPE="${TYPE:-full}"

echo "=== pg-full-bm backup ==="

# Verify container is running
docker inspect "$CONTAINER" >/dev/null 2>&1 || {
  echo "ERROR: container $CONTAINER not found"
  exit 1
}

# Ensure stanza exists (stanza-upgrade if system-id changed)
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
  pgbackrest --stanza="$STANZA" stanza-create 2>&1 | tail -1 || {
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
    pgbackrest --stanza="$STANZA" stanza-upgrade 2>&1 | tail -1
}

# Fix ownership so postgres user can archive WAL
docker exec "$CONTAINER" chown -R 70:70 /var/lib/pgbackrest 2>/dev/null

# Run pgbackrest backup
echo "Backing up (type=$TYPE, stanza=$STANZA)..."
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
  pgbackrest --stanza="$STANZA" --type="$TYPE" --no-archive-check backup 2>&1 || {
  # Retry with stanza-upgrade if system-id mismatch
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
    pgbackrest --stanza="$STANZA" stanza-upgrade 2>&1 | tail -1
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
    pgbackrest --stanza="$STANZA" --type="$TYPE" --no-archive-check backup 2>&1
}
docker exec "$CONTAINER" chown -R 70:70 /var/lib/pgbackrest 2>/dev/null

# Show last backup info
echo ""
echo "=== backup complete ==="
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
  pgbackrest --stanza="$STANZA" info 2>&1 | tail -3
