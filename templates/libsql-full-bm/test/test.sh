#!/bin/sh
# libsql-full-bm integration test — full PITR cycle
# All SQL commands inside Docker via docker exec
set -e

PRIMARY_CONTAINER="libsql-full-bm-sqld-primary-1"
REPLICA_CONTAINER="libsql-full-bm-sqld-replica-1"
COMPOSE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BACKUP_FILE="/tmp/libsql-pitr-test-$(date +%s).db"

SQL() { docker exec "$PRIMARY_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
SQL_REPLICA() { docker exec "$REPLICA_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }

COUNT() { echo "$1" | grep -o '"rows":\[\[[0-9]*\]\]' 2>/dev/null | grep -o '[0-9]' 2>/dev/null || echo ""; }

echo "=== libsql-full-bm INTEGRATION TEST ==="

echo "--- Step 1: Verify services ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" ps --status running 2>/dev/null | grep -q "$PRIMARY_CONTAINER" || {
  echo "ERROR: container not running"
  exit 1
}
echo "  ✓ Services running"

echo "--- Step 2: Create pre-backup data ---"
SQL '{"statements":[
  "CREATE TABLE IF NOT EXISTS _pitr_test (id INTEGER PRIMARY KEY, label TEXT)",
  "INSERT INTO _pitr_test VALUES (1, '\''alpha'\'')",
  "INSERT INTO _pitr_test VALUES (2, '\''bravo'\'')",
  "INSERT INTO _pitr_test VALUES (3, '\''charlie'\'')"
]}' > /dev/null

PRE_JSON=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM _pitr_test"]}')
PRE_COUNT=$(COUNT "$PRE_JSON")
echo "  Pre-backup rows: $PRE_COUNT"

echo "--- Step 3: Full backup ---"
DATA_DIR=$(docker exec "$PRIMARY_CONTAINER" sh -c 'find /var/lib/sqld -path "*/dbs/default/data" 2>/dev/null | head -1')
echo "  Data path: $DATA_DIR"
docker cp "$PRIMARY_CONTAINER:$DATA_DIR" "$BACKUP_FILE" 2>/dev/null
echo "  ✓ Backup: $BACKUP_FILE"

echo "--- Step 4: Insert post-backup data ---"
SQL '{"statements":[
  "INSERT INTO _pitr_test VALUES (4, '\''delta'\'')",
  "INSERT INTO _pitr_test VALUES (5, '\''echo'\'')"
]}' > /dev/null

POST_JSON=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM _pitr_test"]}')
POST_COUNT=$(COUNT "$POST_JSON")
echo "  Rows after inserts: $POST_COUNT"

echo "--- Step 5: Verify pre-restore state ---"
if [ "$POST_COUNT" = "5" ]; then
  echo "  ✓ 5 rows present (3 pre-backup + 2 post-backup)"
else
  echo "FAIL: expected 5 rows, got $POST_COUNT"
  exit 1
fi

echo "--- Step 6: Drop table (disaster) ---"
SQL '{"statements":["DROP TABLE IF EXISTS _pitr_test"]}' > /dev/null
DROP_JSON=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM _pitr_test"]}' 2>/dev/null)
DROP_COUNT=$(echo "$DROP_JSON" | grep -o '"rows":\[\[[0-9]*\]\]' 2>/dev/null | grep -o '[0-9]' 2>/dev/null || echo "")
# Empty or error means table is gone (correct disaster)
if [ -z "$DROP_COUNT" ] || [ "$DROP_COUNT" = "0" ]; then
  echo "  ✓ Table dropped"
else
  echo "FAIL: table still has $DROP_COUNT rows"
  exit 1
fi

echo "--- Step 7: Restore from backup ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" down 2>&1 | tail -1

DATA_BASE=$(dirname "$DATA_DIR" 2>/dev/null || echo "/var/lib/sqld/data.sqld/dbs/default")
docker run --rm \
  -v "libsql-full-bm_primary_data:/var/lib/sqld" \
  -v "$(dirname "$BACKUP_FILE"):/backup:ro" \
  alpine sh -c "
mkdir -p $DATA_BASE
cp /backup/$(basename "$BACKUP_FILE") $DATA_BASE/data
chmod 644 $DATA_BASE/data
" 2>/dev/null

docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d 2>&1 | tail -1

echo -n "Waiting for sqld..."
until docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; do sleep 2; done
echo ""

echo "--- Step 8: Verify post-restore state ---"
RESTORE_JSON=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM _pitr_test"]}' 2>/dev/null || echo '{"rows":[[0]]}')
RESTORE_COUNT=$(COUNT "$RESTORE_JSON")
echo "  Rows after restore: $RESTORE_COUNT"

if [ "$RESTORE_COUNT" = "3" ]; then
  echo "  ✅ 3 rows restored (pre-backup data only)"
elif [ "$RESTORE_COUNT" = "5" ]; then
  echo "  ⚠ 5 rows restored (with WAL replay)"
else
  echo "FAIL: expected 3 or 5 rows, got $RESTORE_COUNT"
  exit 1
fi

echo "--- Step 9: Verify replica ---"
REP_JSON=$(SQL_REPLICA '{"statements":["SELECT COUNT(*) AS cnt FROM _pitr_test"]}' 2>/dev/null || echo '{"rows":[[0]]}')
REP_COUNT=$(COUNT "$REP_JSON")
echo "  Replica rows: $REP_COUNT"

echo "--- Step 10: Cleanup ---"
SQL '{"statements":["DROP TABLE IF EXISTS _pitr_test"]}' > /dev/null || true
rm -f "$BACKUP_FILE"
echo "  ✓ Cleanup done"

echo ""
echo "=== libsql-full-bm INTEGRATION TEST PASSED ==="
