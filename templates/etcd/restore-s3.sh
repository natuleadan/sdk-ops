#!/bin/sh
# etcd restore-s3 — download snapshot from S3 and restore the local member.
#
# 3-MEMBER COORDINATION: all members must go DOWN, then EACH member restores
# from the SAME snapshot with ITS OWN --name and the same --initial-cluster,
# then all start together. This script restores ONE member (the local one).
# Requires: ETCD_NAME and ETCD_INITIAL_CLUSTER.
set -u

BACKUP_DIR="${BACKUP_DIR:-./backups}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
ETCD_IMAGE="quay.io/coreos/etcd:v3.5.15"
ETCD_NAME="${ETCD_NAME:-}"
ETCD_INITIAL_CLUSTER="${ETCD_INITIAL_CLUSTER:-}"
DATA_DIR="${DATA_DIR:-./data/etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_CFG="${S3_CFG:-$HOME/.s3cfg}"
RESTORE_FILE=""
YES=false

usage() {
  echo "Usage: restore-s3.sh [--yes] [snapshot-name]"
  echo ""
  echo "If no snapshot-name given, restores the latest from S3."
  echo "Env: ETCD_NAME + ETCD_INITIAL_CLUSTER required."
  echo ""
  echo "Options:"
  echo "  --yes    Skip confirmation prompt"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_FILE="$1"; shift ;;
  esac
done

command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not installed"; exit 1; }
[ -n "$ETCD_NAME" ]              || { echo "ERROR: ETCD_NAME must be set"; exit 1; }
[ -n "$ETCD_INITIAL_CLUSTER" ]   || { echo "ERROR: ETCD_INITIAL_CLUSTER must be set"; exit 1; }

ensure_s3cfg() {
  # An existing ~/.s3cfg (operator-managed) always wins.
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

echo "=== etcd restore-s3 ==="

ensure_s3cfg

echo "--- Step 1: Find snapshot ---"
if [ -z "$RESTORE_FILE" ]; then
  echo "  Finding latest snapshot in S3..."
  RESTORE_FILE=$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | grep '\.db' | awk '{print $4}' | sort -r | head -1 | xargs -r basename)
  [ -n "$RESTORE_FILE" ] || { echo "  ERROR: no snapshots in s3://$S3_BUCKET/$S3_PREFIX/"; exit 1; }
  echo "  Latest: $RESTORE_FILE"
fi

echo "--- Step 2: Download ---"
mkdir -p "$BACKUP_DIR"
s3cmd get --force "s3://$S3_BUCKET/$S3_PREFIX/$RESTORE_FILE" "$BACKUP_DIR/$RESTORE_FILE" 2>&1
[ -f "$BACKUP_DIR/$RESTORE_FILE" ] || { echo "  ERROR: download failed"; exit 1; }

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: this stops the local etcd member and REPLACES its data."
  echo "All cluster members must be stopped and restored from the SAME snapshot."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "--- Step 3: Stop local member ---"
LOCAL_CONTAINER=$(docker ps -aq --format '{{"{{"}}.Names{{"}}"}}' 2>/dev/null | grep -E 'etcd' | head -1)
[ -n "$LOCAL_CONTAINER" ] && docker stop "$LOCAL_CONTAINER" >/dev/null 2>&1

echo "--- Step 4: Restore data dir (etcdctl snapshot restore) ---"
RESTORE_DIR="$DATA_DIR.restore"
rm -rf "$RESTORE_DIR"
docker run --rm --network host \
  -v "$(realpath "$BACKUP_DIR"):/backup:ro" \
  -v "$(realpath "$(dirname "$DATA_DIR")"):/restore" \
  "$ETCD_IMAGE" etcdctl snapshot restore /backup/$RESTORE_FILE \
    --name "$ETCD_NAME" \
    --initial-cluster "$ETCD_INITIAL_CLUSTER" \
    --initial-advertise-peer-urls "$(echo "$ETCD_INITIAL_CLUSTER" | tr ',' '\n' | grep "^$ETCD_NAME=" | cut -d= -f2)" \
    --data-dir "/restore/$(basename "$DATA_DIR").restore" >/dev/null 2>&1 || {
  echo "  ERROR: snapshot restore failed"
  [ -n "$LOCAL_CONTAINER" ] && docker start "$LOCAL_CONTAINER" >/dev/null 2>&1
  exit 1
}
rm -rf "$DATA_DIR"
mv "$RESTORE_DIR" "$DATA_DIR"
echo "  Data dir restored: $DATA_DIR"

echo "--- Step 5: Start local member ---"
[ -n "$LOCAL_CONTAINER" ] && docker start "$LOCAL_CONTAINER" >/dev/null 2>&1
echo -n "  Waiting for health..."
i=0
while [ "$i" -lt 30 ]; do
  docker exec "$LOCAL_CONTAINER" etcdctl --endpoints=127.0.0.1:2379 endpoint health 2>/dev/null | grep -q healthy && break
  i=$((i + 1))
  sleep 2
done
if docker exec "$LOCAL_CONTAINER" etcdctl --endpoints=127.0.0.1:2379 endpoint health 2>/dev/null | grep -q healthy; then
  echo ""
  echo "  [OK] member healthy after restore"
else
  echo ""
  echo "  [WARN] member not healthy yet — restore all members before starting"
fi

echo ""
echo "=== restore-s3 complete ==="
echo "  Verify: bash validate.sh"
