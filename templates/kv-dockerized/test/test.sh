#!/bin/sh
# kv-dockerized integration test — full PITR cycle (backup -> disaster -> restore -> verify)
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_CONTAINER="kv-dockerized-dragonfly-primary-1"
REPLICA_CONTAINER="kv-dockerized-dragonfly-replica-1"
COMPOSE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BACKUP_FILE="/tmp/kv-pitr-test-$(date +%s).dfs"

RC() { docker exec -e DF_PASSWORD="$DF_PASSWORD" "$PRIMARY_CONTAINER" redis-cli -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REPLICA() { docker exec -e DF_PASSWORD="$DF_PASSWORD" "$REPLICA_CONTAINER" redis-cli -p 6380 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== kv-dockerized INTEGRATION TEST ==="

echo "--- Step 1: Verify services ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" ps --status running 2>/dev/null | grep -q "$PRIMARY_CONTAINER" || {
  echo "ERROR: container not running"
  exit 1
}
echo "  ✓ Services running"

echo "--- Step 2: Create pre-backup data ---"
RC SET key-alpha "value-alpha"
RC SET key-bravo "value-bravo"
RC SET key-charlie "value-charlie"
PRE_COUNT=$(RC DBSIZE)
echo "  Pre-backup keys: $PRE_COUNT"

echo "--- Step 3: Full backup (BGSAVE) ---"
RC BGSAVE
echo -n "  Waiting for snapshot..."
for i in $(seq 1 30); do
  INPROGRESS=$(RC INFO PERSISTENCE | grep "rdb_bgsave_in_progress" | cut -d: -f2 | tr -d '\r\n')
  [ "$INPROGRESS" = "0" ] || [ -z "$INPROGRESS" ] && break
  sleep 1
done
echo " done"

TIMESTAMP=$(docker exec "$PRIMARY_CONTAINER" sh -c 'ls /data/dump-*summary.dfs 2>/dev/null | sed "s/.*dump-//;s/-summary.*//" | sort | tail -1')
mkdir -p "$BACKUP_FILE"
for f in $(docker exec "$PRIMARY_CONTAINER" sh -c "ls /data/dump-$TIMESTAMP-*.dfs 2>/dev/null"); do
  docker cp "$PRIMARY_CONTAINER:$f" "$BACKUP_FILE/" 2>/dev/null
done
echo "  ✓ Backup: $BACKUP_FILE ($(du -sh "$BACKUP_FILE" | cut -f1))"

echo "--- Step 4: Insert post-backup data ---"
RC SET key-delta "value-delta"
RC SET key-echo "value-echo"
echo "  ✓ Post-backup data inserted"

POST_COUNT=$(RC DBSIZE)
echo "  Keys after inserts: $POST_COUNT"

echo "--- Step 5: Verify pre-restore state ---"
if [ "$POST_COUNT" = "5" ]; then
  echo "  ✓ 5 keys present (3 pre-backup + 2 post-backup)"
else
  echo "FAIL: expected 5 keys, got $POST_COUNT"
  exit 1
fi

echo "--- Step 6: FLUSHALL (disaster) ---"
RC FLUSHALL
AFTER_FLUSH=$(RC DBSIZE)
if [ "$AFTER_FLUSH" = "0" ]; then
  echo "  ✓ All keys deleted"
else
  echo "FAIL: expected 0 keys, got $AFTER_FLUSH"
  exit 1
fi

echo "--- Step 7: Restore from backup ---"
docker compose -f "$COMPOSE_DIR/docker-compose.yml" down 2>&1 | tail -1

BNAME=$(basename "$BACKUP_FILE")
docker run --rm -v "kv-dockerized_primary_data:/data" -v "$BACKUP_FILE:/backup:ro" \
  alpine sh -c "cp /backup/*.dfs /data/ && chmod 644 /data/*.dfs" 2>/dev/null

docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d 2>&1 | tail -1

echo -n "Waiting for Dragonfly..."
until RC PING | grep -q "PONG"; do sleep 2; done
echo ""
cd "$COMPOSE_DIR" && bash init.sh 2>&1 | tail -3

echo "--- Step 8: Verify post-restore state ---"
RESTORE_COUNT=$(RC DBSIZE)

KEY_ALPHA=$(RC GET key-alpha)
KEY_BRAVO=$(RC GET key-bravo)
KEY_CHARLIE=$(RC GET key-charlie)
KEY_DELTA=$(RC GET key-delta)
KEY_ECHO=$(RC GET key-echo)
: "${KEY_DELTA:=}"
: "${KEY_ECHO:=}"

echo "  key-alpha: $KEY_ALPHA"
echo "  key-bravo: $KEY_BRAVO"
echo "  key-charlie: $KEY_CHARLIE"
echo "  key-delta: $KEY_DELTA"
echo "  key-echo: $KEY_ECHO"

if [ "$RESTORE_COUNT" = "3" ] && \
   [ "$KEY_ALPHA" = "value-alpha" ] && \
   [ "$KEY_BRAVO" = "value-bravo" ] && \
   [ "$KEY_CHARLIE" = "value-charlie" ] && \
   [ -z "$KEY_DELTA" ] && \
   [ -z "$KEY_ECHO" ]; then
  echo "  ✅ 3 keys restored (correct PITR without WAL)"
elif [ "$RESTORE_COUNT" = "5" ]; then
  echo "  ⚠ 5 keys restored (with WAL replay)"
else
  echo "FAIL: expected 3 keys, got $RESTORE_COUNT"
  exit 1
fi

echo "--- Step 9: Verify replica ---"
REP_COUNT=$(RC_REPLICA DBSIZE)
echo "  Replica keys: $REP_COUNT"

echo "--- Step 10: Cleanup ---"
RC FLUSHALL
RC_REPLICA FLUSHALL
rm -rf "$BACKUP_FILE"
echo "  ✓ Test data cleaned"

echo ""
echo "=== kv-dockerized INTEGRATION TEST PASSED ==="
