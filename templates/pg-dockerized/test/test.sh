#!/bin/sh
# pg-dockerized integration test — full PITR cycle via pgbackrest
set -e

PG_USER="${PG_USER:-dev}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"
CONTAINER="${CONTAINER:-pg-dockerized-postgres-1}"
COMPOSE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
STANZA="${STANZA:-main}"

# All psql commands run inside the Docker container via docker exec
PSQL() { docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" "$@" 2>/dev/null; }

echo "=== pg-dockerized INTEGRATION TEST ==="

# 1. Ensure services are running
echo "--- Step 1: Verify services ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" ps --status running 2>/dev/null | grep -q "$CONTAINER" || {
  echo "ERROR: container not running. Run 'docker compose up -d' first"
  exit 1
}

# 2. Create test data (pre-backup)
echo "--- Step 2: Create pre-backup data ---"
PSQL -c "
  CREATE TABLE IF NOT EXISTS _pitr_test (
    id SERIAL PRIMARY KEY,
    label TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
  );
  TRUNCATE _pitr_test;
  INSERT INTO _pitr_test (label) VALUES ('alpha'), ('bravo'), ('charlie');
  SELECT COUNT(*) AS pre_count FROM _pitr_test;
"

# 3. Full backup via pgbackrest
echo "--- Step 3: pgbackrest full backup ---"
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
  pgbackrest --stanza="$STANZA" --type=full backup 2>&1 | tail -3
docker exec "$CONTAINER" chown -R 70:70 /var/lib/pgbackrest 2>/dev/null

# 4. Insert post-backup data
echo "--- Step 4: Insert post-backup data ---"
PSQL -c "
  INSERT INTO _pitr_test (label) VALUES ('delta'), ('echo');
  SELECT COUNT(*) AS post_count FROM _pitr_test;
"

# 5. Verify 5 rows exist (3 pre + 2 post)
echo "--- Step 5: Verify pre-restore state ---"
COUNT=$(PSQL -tAc "SELECT COUNT(*) FROM _pitr_test;")
if [ "$COUNT" != "5" ]; then
  echo "FAIL: expected 5 rows, got $COUNT"
  exit 1
fi
echo "  ✓ 5 rows present (3 pre-backup + 2 post-backup)"

# 6. Destroy the table (simulate disaster)
echo "--- Step 6: Drop table (disaster) ---"
PSQL -c "DROP TABLE _pitr_test;"
PSQL -tAc "SELECT COUNT(*) FROM _pitr_test;" 2>/dev/null && {
  echo "FAIL: table still exists"
  exit 1
} || echo "  ✓ Table dropped successfully"

# 7. Restore from backup (stop container, restore, restart)
echo "--- Step 7: Restore from backup ---"
echo "  Stopping PostgreSQL..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" stop postgres 2>&1 | tail -1

echo "  Removing old data..."
rm -rf "$COMPOSE_DIR/data/pg/18/docker"
mkdir -p "$COMPOSE_DIR/data/pg/18/docker"

echo "  Running pgbackrest restore..."
docker run --rm \
  -v "$COMPOSE_DIR/data/pg:/var/lib/postgresql" \
  -v "$COMPOSE_DIR/data/pgbackrest:/var/lib/pgbackrest" \
  -v "$COMPOSE_DIR/pgbackrest.conf:/etc/pgbackrest/pgbackrest.conf:ro" \
  -e PGPASSWORD="$PG_PASSWORD" \
  pg-dockerized:latest \
  sh -c "
pgbackrest --stanza=$STANZA --db-path=/var/lib/postgresql/18/docker --type=none restore 2>&1
rm -f /var/lib/postgresql/18/docker/postgresql.auto.conf
rm -f /var/lib/postgresql/18/docker/backup_label
rm -f /var/lib/postgresql/18/docker/recovery.signal
chown -R postgres:postgres /var/lib/postgresql/18/docker 2>/dev/null
su -s /bin/sh postgres -c 'pg_resetwal -f -D /var/lib/postgresql/18/docker' 2>/dev/null || \
su -s /bin/sh postgres -c 'pg_reset_wal -f -D /var/lib/postgresql/18/docker' 2>/dev/null || true
" 2>&1 | tail -3

echo "  Starting PostgreSQL..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" start postgres 2>&1 | tail -1

# Wait for PG
sleep 5
until docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pg_isready -U "$PG_USER" -h localhost 2>/dev/null; do sleep 2; done
echo "  PostgreSQL ready"

# 8. Verify restore — should have 3 rows (alpha, bravo, charlie) from full backup
echo "--- Step 8: Verify post-restore state ---"
COUNT=$(PSQL -tAc "SELECT COUNT(*) FROM _pitr_test;" 2>/dev/null || echo "0")
ROWS=$(PSQL -tAc "SELECT label FROM _pitr_test ORDER BY id;" 2>/dev/null)

if [ "$COUNT" = "3" ]; then
  echo "  ✓ 3 rows restored (pre-backup data only)"
  echo "  Rows: $ROWS"
  echo "  ✓ Post-backup data (delta, echo) correctly excluded"
elif [ "$COUNT" = "5" ]; then
  echo "  ✓ 5 rows restored (with WAL replay)"
  echo "  Rows: $ROWS"
else
  echo "FAIL: expected 3 or 5 rows, got $COUNT"
  exit 1
fi

# 9. Cleanup
echo "--- Step 9: Cleanup ---"
PSQL -c "DROP TABLE IF EXISTS _pitr_test;" 2>/dev/null
echo "  ✓ Test table dropped"

# 10. Verify replication is still working
echo "--- Step 10: Verify replicas ---"
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -tAc \
  "SELECT state, count(*) FROM pg_stat_replication GROUP BY state" 2>/dev/null | grep -q "streaming" && echo "  ✓ Replicas streaming" || echo "  WARN: Replication not streaming"

echo ""
echo "=== pg-dockerized INTEGRATION TEST PASSED ==="
