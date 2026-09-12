#!/bin/sh
# etcd-bare restore-s3 - download a snapshot from S3, verify it, restore the
# data dir and swap the running member onto it (bare metal, no Docker).
# Uses s3cmd on the host (~/.s3cfg). Credentials from S3_* env. Run as root.
#
# 3-MEMBER COORDINATION (read before running): a shared etcd DCS cannot be
# restored member-by-member while others run - all members must go DOWN, then
# EACH member restores from the SAME snapshot with ITS OWN identity (name,
# initial-cluster and peer URL are parsed from THIS member's etcd.conf.yml),
# then all start together (fresh cluster with the old data). This script
# restores ONE member (the local one). Coordinate the other members yourself:
#   1. Stop etcd on every member (systemctl stop etcd).
#   2. Run restore-s3.sh on EACH member (same snapshot, per-member conf).
#   3. Start etcd on every member (systemctl start etcd).
set -u

CONF="${ETCD_BARE_DIR:-/opt/sdk-ops/services/etcd-bare}/etcd.conf.yml"
CTL="${ETCDCTL:-/usr/local/bin/etcdctl}"
DATA_DIR="/var/lib/etcd"
RESTORE_DIR="/var/lib/etcd-restore"
BACKUP_DIR="${BACKUP_DIR:-/opt/backups/etcd}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_CFG="${S3_CFG:-$HOME/.s3cfg}"
RESTORE_FILE=""
YES=false
export ETCDCTL_API=3

command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not installed"; exit 1; }
[ -f "$CONF" ] || { echo "ERROR: $CONF missing (render + upload first)"; exit 1; }

# Member identity from the rendered etcd.conf.yml - nothing hardcoded.
conf_val() {
  sed -n "s/^$1:[[:space:]]*//p" "$CONF" | head -1 | tr -d '"' | tr -d "'" | tr -d ' '
}
ETCD_NAME=$(conf_val name)
INITIAL_CLUSTER=$(conf_val initial-cluster)
INITIAL_ADV_PEER=$(conf_val initial-advertise-peer-urls)
[ -n "$ETCD_NAME" ]              || { echo "ERROR: cannot parse member name from $CONF"; exit 1; }
[ -n "$INITIAL_CLUSTER" ]        || { echo "ERROR: cannot parse initial-cluster from $CONF"; exit 1; }
[ -n "$INITIAL_ADV_PEER" ]       || { echo "ERROR: cannot parse initial-advertise-peer-urls from $CONF"; exit 1; }

ensure_s3cfg() {
  if [ -s "$S3_CFG" ]; then
    return 0
  fi
  if [ -z "$S3_ENDPOINT" ] || [ -z "$S3_ACCESS_KEY" ] || [ -z "$S3_SECRET_KEY" ]; then
    echo "ERROR: $S3_CFG missing and S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
    return 1
  fi
  umask 077
  cat > "$S3_CFG" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $S3_ENDPOINT
host_bucket = %(bucket)s.$S3_ENDPOINT
use_https = True
EOF
  echo "  -> wrote $S3_CFG from S3_* env"
}

S3="s3cmd -c $S3_CFG"

usage() {
  echo "Usage: restore-s3.sh [--yes|-y] [snapshot-name]"
  echo ""
  echo "If no snapshot-name is given, restores the latest from S3."
  echo ""
  echo "Options:"
  echo "  --yes|-y    Skip confirmation prompt"
  echo "  --help|-h   Show this help"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_FILE="$1"; shift ;;
  esac
done

echo "=== etcd-bare restore-s3 ==="
echo "  member: $ETCD_NAME  peer: $INITIAL_ADV_PEER  data-dir: $DATA_DIR"

echo "--- Step 1: Find snapshot ---"
ensure_s3cfg
if [ -z "$RESTORE_FILE" ]; then
  echo "  Finding latest snapshot in S3..."
  RESTORE_FILE=$($S3 ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | awk '{print $NF}' | grep '\.db$' | sort | sed 's|.*/||' | tail -1 || true)
  if [ -z "$RESTORE_FILE" ]; then
    echo "  ERROR: no snapshots found in s3://$S3_BUCKET/$S3_PREFIX/"
    exit 1
  fi
  echo "  Latest: $RESTORE_FILE"
fi

echo "--- Step 2: Download ---"
mkdir -p "$BACKUP_DIR"
if ! $S3 --no-progress get "s3://$S3_BUCKET/$S3_PREFIX/$RESTORE_FILE" "$BACKUP_DIR/$RESTORE_FILE" --force >/dev/null 2>&1; then
  echo "  ERROR: download failed"
  exit 1
fi
LOCAL_SNAP="$BACKUP_DIR/$RESTORE_FILE"
[ -f "$LOCAL_SNAP" ] || { echo "  ERROR: local copy missing"; exit 1; }
echo "  Downloaded: $LOCAL_SNAP ($(du -h "$LOCAL_SNAP" | cut -f1))"

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: this stops the local etcd member and REPLACES its data."
  echo "All cluster members must be stopped and restored from the SAME snapshot."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "--- Step 3: Verify snapshot integrity ---"
if ! "$CTL" snapshot status "$LOCAL_SNAP" 2>/dev/null | grep -q totalKey; then
  echo "  ERROR: snapshot corrupt/unreadable (etcdctl snapshot status)"
  exit 1
fi
echo "  Snapshot readable (totalKey present)"

echo "--- Step 4: Stop local member ---"
systemctl stop etcd

echo "--- Step 5: Restore data dir (etcdctl snapshot restore) ---"
# snapshot restore builds a fresh data dir for a NEW cluster bootstrapped with
# the ORIGINAL member list - this is why name/initial-cluster/peer URL must
# match the member being restored (parsed from this member's etcd.conf.yml).
rm -rf "$RESTORE_DIR"
if ! "$CTL" snapshot restore "$LOCAL_SNAP" \
      --name "$ETCD_NAME" \
      --initial-cluster "$INITIAL_CLUSTER" \
      --initial-advertise-peer-urls "$INITIAL_ADV_PEER" \
      --data-dir "$RESTORE_DIR" >/dev/null 2>&1; then
  echo "  ERROR: snapshot restore failed - restarting the member as it was"
  systemctl start etcd
  exit 1
fi
echo "  Restored: $RESTORE_DIR"

echo "--- Step 6: Swap the data dir ---"
BAK_DIR="$DATA_DIR.bak-$(date +%F-%H%M%S)"
mv "$DATA_DIR" "$BAK_DIR" 2>/dev/null || true
mv "$RESTORE_DIR" "$DATA_DIR"
chown -R etcd:etcd "$DATA_DIR"
echo "  Old data kept: $BAK_DIR"

echo "--- Step 7: Start local member ---"
systemctl start etcd
echo -n "  Waiting for health..."
i=0
while [ "$i" -lt 30 ]; do
  if "$CTL" --endpoints=127.0.0.1:2379 --command-timeout=6s endpoint health 2>/dev/null | grep -q healthy; then
    break
  fi
  i=$((i + 1))
  sleep 2
done
if "$CTL" --endpoints=127.0.0.1:2379 --command-timeout=6s endpoint health 2>/dev/null | grep -q healthy; then
  echo ""
  echo "  [OK] member healthy after restore"
  KEYS=$("$CTL" --endpoints=127.0.0.1:2379 get "" --prefix --keys-only 2>/dev/null | grep -c . || true)
  echo "  Keys visible after restore: ${KEYS:-0}"
else
  echo ""
  echo "  [WARN] member not healthy yet - if other members are still down, this is expected until ALL members are restored and started (quorum 2/3)"
fi
rm -f "$LOCAL_SNAP"

echo ""
echo "=== restore-s3 complete ==="
echo "  Verify: bash validate.sh"
