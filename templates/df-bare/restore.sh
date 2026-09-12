#!/bin/sh
# df-bare restore - restore the primary from a local snapshot dir or .dfs file.
# Stops the cluster, places the snapshot files into the primary data dir and
# restarts (Dragonfly auto-loads the newest snapshot; the replicas resync via
# --replicaof). Run as root.
set -e

DF_PASSWORD="${DF_PASSWORD:-}"
if [ -z "$DF_PASSWORD" ] && [ -r /etc/dragonfly/primary.conf ]; then
  DF_PASSWORD="$(sed -n 's/^--requirepass=//p' /etc/dragonfly/primary.conf | head -1)"
fi
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_DATA="${PRIMARY_DATA:-/var/lib/dragonfly/primary}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
YES=false
SNAPSHOT=""

RC() { redis-cli -h 127.0.0.1 -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

usage() {
  echo "Usage: restore.sh [--yes] <snapshot-dir-or-file>"
  echo "Restore the primary from a local snapshot (dir of .dfs files or one file)"
  echo ""
  echo "Options:"
  echo "  --yes    Skip confirmation prompt"
  echo "  --help   Show this help"
  echo ""
  echo "Examples:"
  echo "  restore.sh backups/2026-09-02-030000"
  echo "  restore.sh --yes /path/to/dump.dfs"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes) YES=true; shift ;;
    --help|-h) usage ;;
    *) SNAPSHOT="$1"; shift ;;
  esac
done

if [ -z "$SNAPSHOT" ] || [ ! -e "$SNAPSHOT" ]; then
  echo "Usage: restore.sh [--yes] <snapshot-dir-or-file>"
  echo ""
  echo "Available local backups:"
  ls -lh "$BACKUP_DIR" 2>/dev/null || echo "  (no backups found)"
  exit 1
fi

echo "=== df-bare restore ==="
echo "  Snapshot: $SNAPSHOT"

if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop the cluster and replace all primary data."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "Stopping Dragonfly units..."
systemctl stop dragonfly-primary dragonfly-replica-1 dragonfly-replica-2

echo "Placing snapshot into $PRIMARY_DATA..."
if [ -d "$SNAPSHOT" ]; then
  SRC_DIR="$(cd "$SNAPSHOT" && pwd)"
  cp "$SRC_DIR"/*.dfs "$PRIMARY_DATA/" 2>/dev/null || { echo "ERROR: no .dfs files in $SRC_DIR"; exit 1; }
else
  cp "$SNAPSHOT" "$PRIMARY_DATA/dump.dfs"
fi
chown dragonfly:dragonfly "$PRIMARY_DATA"/*.dfs 2>/dev/null || true
chmod 644 "$PRIMARY_DATA"/*.dfs 2>/dev/null || true
echo "  Data dir ready"

echo "Starting Dragonfly (auto-loads the newest snapshot)..."
systemctl start dragonfly-primary dragonfly-replica-1 dragonfly-replica-2

echo -n "Waiting for primary..."
i=0
while [ "$i" -lt 60 ]; do
  RC PING 2>/dev/null | grep -q "PONG" && break
  i=$((i + 1))
  sleep 2
done
RC PING 2>/dev/null | grep -q "PONG" || { echo " FAIL"; echo "ERROR: primary did not come back after restore"; exit 1; }
echo " OK"

KEYS=$(RC DBSIZE)
if [ -n "$KEYS" ]; then
  echo "  Keys restored: $KEYS"
else
  echo "  FAIL: no response"
  exit 1
fi

# Re-apply REPLICAOF (only if a replica is not streaming) + fresh cluster IDs.
DIR="$(cd "$(dirname "$0")" && pwd)"
bash "$DIR/init.sh" 2>&1 | tail -5

echo ""
echo "=== restore complete ==="
echo "  Verify: bash validate.sh"
