#!/bin/sh
# df-bare restore-s3 - download backup from S3 and restore (bare metal).
# Uses s3cmd on the host (~/.s3cfg, like the other bare templates). Stops the
# cluster, wipes the data dirs, extracts the snapshot into the primary and
# restarts (the replicas resync via --replicaof). Run as root.
set -e

DF_PASSWORD="${DF_PASSWORD:-}"
if [ -z "$DF_PASSWORD" ] && [ -r /etc/dragonfly/primary.conf ]; then
  DF_PASSWORD="$(sed -n 's/^--requirepass=//p' /etc/dragonfly/primary.conf | head -1)"
fi
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_DATA="${PRIMARY_DATA:-/var/lib/dragonfly/primary}"
REPLICA1_DATA="${REPLICA1_DATA:-/var/lib/dragonfly/replica-1}"
REPLICA2_DATA="${REPLICA2_DATA:-/var/lib/dragonfly/replica-2}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
S3_BUCKET="${S3_BUCKET:-df-backups}"
S3_PREFIX="${S3_PREFIX:-df}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_CFG="${S3_CFG:-$HOME/.s3cfg}"
RESTORE_FILE=""
YES=false

RC() { redis-cli -h 127.0.0.1 -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not installed (run init.sh)"; exit 1; }
command -v redis-cli >/dev/null 2>&1 || { echo "ERROR: redis-cli not installed (run init.sh)"; exit 1; }

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

S3="s3cmd -c $S3_CFG"

usage() {
  echo "Usage: restore-s3.sh [--yes|-y] [backup-name]"
  echo ""
  echo "If no backup-name is given, restores the latest from S3."
  echo ""
  echo "Options:"
  echo "  --yes|-y    Skip confirmation prompt"
  echo "  --help|-h   Show this help"
  echo ""
  echo "Examples:"
  echo "  restore-s3.sh                            # latest from S3"
  echo "  restore-s3.sh --yes df-2026-09-02-030000.tar.gz  # specific backup"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=true; shift ;;
    --help|-h) usage ;;
    *) RESTORE_FILE="$1"; shift ;;
  esac
done

echo "=== df-bare restore-s3 ==="

echo "--- Step 1: Find backup ---"
ensure_s3cfg
if [ -z "$RESTORE_FILE" ]; then
  echo "  Finding latest backup in S3..."
  RESTORE_FILE=$($S3 ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | awk '{print $NF}' | grep '\.tar\.gz$' | sort | sed 's|.*/||' | tail -1 || true)
  if [ -z "$RESTORE_FILE" ]; then
    echo "  ERROR: no backups found in s3://$S3_BUCKET/$S3_PREFIX/"
    exit 1
  fi
  echo "  Latest: $RESTORE_FILE"
fi

echo "--- Step 2: Download from S3 ---"
mkdir -p "$BACKUP_DIR"
if ! $S3 --no-progress get "s3://$S3_BUCKET/$S3_PREFIX/$RESTORE_FILE" "$BACKUP_DIR/$RESTORE_FILE" --force >/dev/null 2>&1; then
  echo "  ERROR: download failed"
  exit 1
fi
LOCAL_BACKUP="$BACKUP_DIR/$RESTORE_FILE"
if [ ! -f "$LOCAL_BACKUP" ]; then
  echo "  ERROR: download failed"
  exit 1
fi
echo "  Downloaded: $LOCAL_BACKUP ($(du -h "$LOCAL_BACKUP" | cut -f1))"

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop the cluster and replace all data."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "--- Step 3: Stop all Dragonfly units ---"
systemctl stop dragonfly-primary dragonfly-replica-1 dragonfly-replica-2

echo "--- Step 4: Restore snapshot into the primary data dir ---"
# The tar contains dump-<ts>-*.dfs files; Dragonfly auto-loads the newest set
# on start. The replicas are wiped too: they resync from the primary.
find "$PRIMARY_DATA" -mindepth 1 -delete 2>/dev/null || true
find "$REPLICA1_DATA" -mindepth 1 -delete 2>/dev/null || true
find "$REPLICA2_DATA" -mindepth 1 -delete 2>/dev/null || true
tar xzf "$LOCAL_BACKUP" -C "$PRIMARY_DATA"
chown -R dragonfly:dragonfly "$PRIMARY_DATA" "$REPLICA1_DATA" "$REPLICA2_DATA"
chmod 644 "$PRIMARY_DATA"/*.dfs 2>/dev/null || true
echo "  Data restored (primary gets the snapshot, replicas resync on start)"

echo "--- Step 5: Start services ---"
systemctl start dragonfly-primary dragonfly-replica-1 dragonfly-replica-2

echo -n "Waiting for Dragonfly..."
i=0
while [ "$i" -lt 60 ]; do
  RC PING 2>/dev/null | grep -q "PONG" && break
  i=$((i + 1))
  sleep 2
done
RC PING 2>/dev/null | grep -q "PONG" || {
  echo " FAIL"
  echo "ERROR: Dragonfly did not come back after restore"
  exit 1
}
echo " OK"

# Replication + cluster config must be re-applied after a full restart.
DIR="$(cd "$(dirname "$0")" && pwd)"
bash "$DIR/init.sh" 2>&1 | tail -3

KEYS=$(RC DBSIZE)
echo "  Keys restored: ${KEYS:-0}"

echo ""
echo "=== restore-s3 complete ==="
echo "  Run 'bash validate.sh' for the full health check"
