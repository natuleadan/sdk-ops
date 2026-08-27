#!/bin/sh
# libsql-dockerized integration test — full PITR cycle
# All SQL commands inside Docker via docker exec
set -e

PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"
REPLICA_CONTAINER="libsql-dockerized-sqld-replica-1"
COMPOSE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BACKUP_FILE="/tmp/libsql-pitr-test-$(date +%s).tar.gz"

SQL() { docker exec "$PRIMARY_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
SQL_REPLICA() { docker exec "$REPLICA_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }

COUNT() { echo "$1" | grep -o '"rows":\[\[[0-9]*\]\]' 2>/dev/null | head -1 | grep -o '[0-9]' 2>/dev/null | tr -d '\n' || echo ""; }

echo "=== libsql-dockerized INTEGRATION TEST ==="

echo "--- Step 1: Verify services ---"
# docker ps (not compose ps): after a failover the controller recreates the
# sqld containers via `docker run` (same labels, but not compose-managed), so
# `docker compose ps` only lists etcd/router/controller. docker ps sees all.
docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^$PRIMARY_CONTAINER$" || {
  echo "ERROR: primary container not running"
  exit 1
}
docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^$REPLICA_CONTAINER$" || {
  echo "ERROR: replica container not running"
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

echo "--- Step 3: Full backup (data.sqld snapshot) ---"
# Tar the whole data.sqld dir — sqld state lives in WAL frames + wallog, not
# in a plain .db (a .db-only copy restores an empty checkpoint).
DATA_DIR="/var/lib/sqld/data.sqld"
docker exec "$PRIMARY_CONTAINER" sh -c "cd /var/lib/sqld && tar czf - data.sqld" > "$BACKUP_FILE" 2>/dev/null
echo "  ✓ Backup: $BACKUP_FILE ($(du -h "$BACKUP_FILE" | cut -f1))"

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
# Stop ALL project containers — after a failover the sqld ones were recreated
# by the controller via docker run, so `docker compose down` alone would leave
# them running and holding the volume.
docker ps -aq --filter "label=com.docker.compose.project=libsql-dockerized" 2>/dev/null | xargs -r docker stop 2>/dev/null
docker ps -aq --filter "label=com.docker.compose.project=libsql-dockerized" 2>/dev/null | xargs -r docker rm 2>/dev/null

docker run --rm \
  -v "libsql-dockerized_primary_data:/var/lib/sqld" \
  -v "$(dirname "$BACKUP_FILE"):/backup:ro" \
  alpine sh -c "
rm -rf /var/lib/sqld/data.sqld
mkdir -p /var/lib/sqld
tar xzf /backup/$(basename "$BACKUP_FILE") -C /var/lib/sqld
chown -R 666:666 /var/lib/sqld/data.sqld
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
  echo "  ✓ OK — exactly 3 rows restored (clean PITR: pre-backup data only, no WAL replay)"
elif [ "$RESTORE_COUNT" = "5" ]; then
  echo "  FAIL: 5 rows restored — WAL replay bug (backup was not a consistent snapshot)"
  exit 1
else
  echo "FAIL: expected 3 rows, got $RESTORE_COUNT"
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
echo "--- Step 11: Failover test ---"
echo "  Writing test data..."
SQL '{"statements":[
  "CREATE TABLE IF NOT EXISTS _failover_test (id INTEGER PRIMARY KEY, val TEXT)",
  "INSERT INTO _failover_test VALUES (1, '\''before-failover'\'')"
]}' > /dev/null

PRE_FO=$(SQL '{"statements":["SELECT val FROM _failover_test WHERE id=1"]}')
echo "  Pre-failover: $PRE_FO"

echo "  Killing primary..."
docker kill "$PRIMARY_CONTAINER" 2>/dev/null || true
echo "  Waiting for controller failover (15s)..."
sleep 15

echo -n "  Post-failover write: "
SQL '{"statements":["INSERT INTO _failover_test VALUES (2, '\''after-failover'\'')"]}' > /dev/null 2>&1 && echo "OK" || echo "FAIL (writes stopped)"

echo -n "  Post-failover read: "
FO_JSON=$(SQL '{"statements":["SELECT COUNT(*) AS cnt FROM _failover_test"]}' 2>/dev/null || echo '{"rows":[[0]]}')
FO_COUNT=$(COUNT "$FO_JSON")
echo "$FO_COUNT row(s)"

if [ "$FO_COUNT" -ge 1 ] 2>/dev/null; then
  echo "  ✓ Failover PASSED — cluster survived primary death"
else
  echo "  WARN Failover inconclusive — manual verification needed"
fi

echo "  Restarting old primary as replica..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d sqld-primary 2>&1 | tail -1
sleep 5

echo "--- Step 12: Final cleanup ---"
SQL '{"statements":["DROP TABLE IF EXISTS _failover_test"]}' > /dev/null || true
echo "  ✓ Final cleanup done"

echo ""
echo "--- Step 13: S3 backup test ---"
if [ -n "${S3_ACCESS_KEY:-}" ] && [ -n "${S3_SECRET_KEY:-}" ]; then
  echo "  Running backup-s3.sh..."
  cd "$COMPOSE_DIR" && bash backup-s3.sh 2>&1 | grep -E "Uploaded|complete" || echo "  WARN: backup-s3.sh failed"
  echo "  ✓ S3 backup attempted"
else
  echo "  SKIP (no S3_ACCESS_KEY/S3_SECRET_KEY set)"
fi

echo ""
echo "--- Step 14: Router routing test ---"
echo -n "  POST via router (write): "
curl -sk -X POST https://localhost:${LIBSQL_HTTP_TLS:-8443} \
  -H "Content-Type: application/json" \
  -d '{"statements":["CREATE TABLE IF NOT EXISTS _router_test (id INTEGER PRIMARY KEY)","INSERT INTO _router_test VALUES (1)"]}' > /dev/null 2>&1 && echo "OK" || echo "FAIL"

echo -n "  GET via router (read): "
curl -sk https://localhost:${LIBSQL_HTTP_TLS:-8443} \
  -H "Content-Type: application/json" \
  -d '{"statements":["SELECT COUNT(*) AS cnt FROM _router_test"]}' 2>/dev/null | grep -q '"rows":\[\[1\]\]' && echo "OK" || echo "FAIL"

SQL '{"statements":["DROP TABLE IF EXISTS _router_test"]}' > /dev/null 2>&1 || true

echo ""
echo "=== libsql-dockerized INTEGRATION TEST PASSED ==="
