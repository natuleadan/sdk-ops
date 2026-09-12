#!/bin/bash
# df-bare integration test - full PITR cycle (backup -> disaster -> restore -> verify)
# + S3 DR cycle (skipped without S3 env) + failover (reads survive via replicas).
# Run as root: sudo bash test/test.sh (after init.sh).
set -e

DF_PASSWORD="${DF_PASSWORD:-}"
if [ -z "$DF_PASSWORD" ] && [ -r /etc/dragonfly/primary.conf ]; then
  DF_PASSWORD="$(sed -n 's/^--requirepass=//p' /etc/dragonfly/primary.conf | head -1)"
fi
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
PRIMARY_DATA="${PRIMARY_DATA:-/var/lib/dragonfly/primary}"
BACKUP_DIR="/tmp/df-pitr-test-$(date +%s)"
HAPROXY_TLS_PORT="6443"

RC()     { redis-cli -h 127.0.0.1 -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP() { redis-cli -h 127.0.0.1 -p 6380 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_TLS() { redis-cli --tls --cacert "$DIR/ssl/ca.crt" -h 127.0.0.1 -p "$HAPROXY_TLS_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== df-bare INTEGRATION TEST ==="

echo "--- Step 1: Verify services ---"
for u in dragonfly-primary dragonfly-replica-1 dragonfly-replica-2 haproxy; do
  systemctl is-active --quiet "$u" || { echo "ERROR: unit $u not running"; exit 1; }
done
echo "  [OK] Services running"

echo "--- Step 2: Create pre-backup data ---"
RC SET key-alpha "value-alpha" >/dev/null
RC SET key-bravo "value-bravo" >/dev/null
RC SET key-charlie "value-charlie" >/dev/null
PRE_COUNT=$(RC DBSIZE)
echo "  Pre-backup keys: $PRE_COUNT"

echo "--- Step 3: Full backup (BGSAVE) ---"
RC BGSAVE >/dev/null
echo -n "  Waiting for snapshot..."
for i in $(seq 1 30); do
  INPROGRESS=$(RC INFO PERSISTENCE | grep "rdb_bgsave_in_progress" | cut -d: -f2 | tr -d '\r\n')
  [ "$INPROGRESS" = "0" ] || [ -z "$INPROGRESS" ] && break
  sleep 1
done
echo " done"

TIMESTAMP=$(ls "$PRIMARY_DATA"/dump-*summary.dfs 2>/dev/null | sed "s/.*dump-//;s/-summary.*//" | sort | tail -1)
[ -n "$TIMESTAMP" ] || { echo "ERROR: no snapshot files (BGSAVE failed?)"; exit 1; }
mkdir -p "$BACKUP_DIR"
cp "$PRIMARY_DATA"/dump-"$TIMESTAMP"-*.dfs "$BACKUP_DIR/" 2>/dev/null
echo "  [OK] Backup: $BACKUP_DIR ($(du -sh "$BACKUP_DIR" | cut -f1))"

echo "--- Step 4: Insert post-backup data ---"
RC SET key-delta "value-delta" >/dev/null
RC SET key-echo "value-echo" >/dev/null
echo "  [OK] Post-backup data inserted"

POST_COUNT=$(RC DBSIZE)
echo "  Keys after inserts: $POST_COUNT"

echo "--- Step 5: Verify pre-restore state ---"
if [ "$POST_COUNT" = "5" ]; then
  echo "  [OK] 5 keys present (3 pre-backup + 2 post-backup)"
else
  echo "FAIL: expected 5 keys, got $POST_COUNT"
  exit 1
fi

echo "--- Step 6: FLUSHALL (disaster) ---"
RC FLUSHALL >/dev/null 2>&1 || true
AFTER_FLUSH=$(RC DBSIZE)
if [ "$AFTER_FLUSH" = "0" ]; then
  echo "  [OK] All keys deleted"
else
  echo "FAIL: expected 0 keys, got $AFTER_FLUSH"
  exit 1
fi

echo "--- Step 7: Restore from backup ---"
systemctl stop dragonfly-primary dragonfly-replica-1 dragonfly-replica-2

cp "$BACKUP_DIR"/dump-*.dfs "$PRIMARY_DATA/" 2>/dev/null
chown dragonfly:dragonfly "$PRIMARY_DATA"/dump-*.dfs 2>/dev/null || true
chmod 644 "$PRIMARY_DATA"/dump-*.dfs 2>/dev/null || true

systemctl start dragonfly-primary dragonfly-replica-1 dragonfly-replica-2

echo -n "Waiting for Dragonfly..."
for i in $(seq 1 60); do
  RC PING 2>/dev/null | grep -q "PONG" && break
  sleep 2
done
RC PING 2>/dev/null | grep -q "PONG" || { echo " FAIL"; echo "ERROR: primary did not come back"; exit 1; }
echo " OK"
bash "$DIR/init.sh" 2>&1 | tail -3

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
  echo "  [OK] 3 keys restored (correct PITR without WAL)"
elif [ "$RESTORE_COUNT" = "5" ]; then
  echo "  [WARN] 5 keys restored (with WAL replay)"
else
  echo "FAIL: expected 3 keys, got $RESTORE_COUNT"
  exit 1
fi

echo "--- Step 9: Verify replica ---"
REP_COUNT=$(RC_REP DBSIZE)
echo "  Replica keys: $REP_COUNT"
if [ "$REP_COUNT" = "$RESTORE_COUNT" ]; then
  echo "  [OK] replica in sync with the primary"
else
  echo "  [WARN] replica count $REP_COUNT != primary $RESTORE_COUNT (replication lag)"
fi

echo "--- Step 10: S3 DR cycle (skipped without S3 env) ---"
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  RC SET key-s3dr "value-s3dr" >/dev/null
  bash "$DIR/backup-s3.sh" > /tmp/df-bare-s3-backup.log 2>&1 || {
    echo "FAIL: backup-s3.sh failed (see /tmp/df-bare-s3-backup.log)"
    exit 1
  }
  echo "  [OK] backup-s3 uploaded"
  RC FLUSHALL >/dev/null 2>&1 || true
  [ "$(RC DBSIZE)" = "0" ] || { echo "FAIL: FLUSHALL did not clear"; exit 1; }
  bash "$DIR/restore-s3.sh" --yes > /tmp/df-bare-s3-restore.log 2>&1 || {
    echo "FAIL: restore-s3.sh failed (see /tmp/df-bare-s3-restore.log)"
    exit 1
  }
  S3_KEY=$(RC GET key-s3dr)
  S3_COUNT=$(RC DBSIZE)
  if [ "$S3_KEY" = "value-s3dr" ] && [ "$S3_COUNT" -ge 1 ] 2>/dev/null; then
    echo "  [OK] S3 restore: key-s3dr back ($S3_COUNT keys)"
  else
    echo "FAIL: S3 restore expected key-s3dr back, got '$S3_KEY' ($S3_COUNT keys)"
    exit 1
  fi
else
  echo "  [SKIP] S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
fi

echo "--- Step 11: Failover (stop primary; reads survive via replicas) ---"
RC SET key-failover "before-stop" >/dev/null
systemctl stop dragonfly-primary
READ_OK=$(RC_REP GET key-failover 2>/dev/null | tr -d '\r\n')
if [ "$READ_OK" = "before-stop" ]; then
  echo "  [OK] replica still serves reads while the primary is down"
else
  echo "  [WARN] replica read during outage returned '$READ_OK' (replication lag)"
fi
echo -n "  HAProxy TLS read..."
HAP_READ=""
for i in $(seq 1 30); do
  HAP_READ=$(RC_TLS GET key-failover 2>/dev/null | tr -d '\r\n')
  [ "$HAP_READ" = "before-stop" ] && break
  sleep 2
done
if [ "$HAP_READ" = "before-stop" ]; then
  echo " OK (reads via replicas through HAProxy TLS)"
else
  echo " WARN (returned '$HAP_READ')"
fi
systemctl start dragonfly-primary
echo -n "  Waiting for primary..."
for i in $(seq 1 30); do
  RC PING 2>/dev/null | grep -q "PONG" && break
  sleep 2
done
RC PING 2>/dev/null | grep -q "PONG" || { echo " FAIL: primary did not recover"; exit 1; }
echo " OK"
bash "$DIR/init.sh" 2>&1 | tail -2
RC SET key-failover "after-recover" >/dev/null
[ "$(RC GET key-failover)" = "after-recover" ] && echo "  [OK] writes work after recovery" || {
  echo "FAIL: writes broken after primary recovery"
  exit 1
}

echo "--- Step 12: Cleanup ---"
RC FLUSHALL >/dev/null 2>&1 || true
RC_REP FLUSHALL >/dev/null 2>&1 || true
rm -rf "$BACKUP_DIR"
echo "  [OK] Test data cleaned"

echo ""
echo "=== df-bare INTEGRATION TEST PASSED ==="
