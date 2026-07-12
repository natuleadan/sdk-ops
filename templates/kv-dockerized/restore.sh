#!/bin/sh
# kv-dockerized restore — restore Dragonfly from .dfs snapshot
# Verification runs inside Docker container
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_CONTAINER="kv-dockerized-dragonfly-primary-1"
COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"
YES=false
SNAPSHOT=""

RC() { docker exec "$PRIMARY_CONTAINER" redis-cli -p 10001 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

usage() {
  echo "Usage: restore.sh [--yes] <snapshot.dfs>"
  echo "Restore Dragonfly from a .dfs snapshot file"
  echo ""
  echo "Options:"
  echo "  --yes    Skip confirmation prompt"
  echo "  --help   Show this help"
  echo ""
  echo "Examples:"
  echo "  restore.sh backups/kv-2026-07-12-030000.dfs"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes) YES=true; shift ;;
    --help|-h) usage ;;
    *) SNAPSHOT="$1"; shift ;;
  esac
done

if [ -z "$SNAPSHOT" ] || [ ! -f "$SNAPSHOT" ]; then
  echo "Usage: restore.sh [--yes] <snapshot.dfs>"
  echo ""
  echo "Available backups:"
  ls -lh backups/ 2>/dev/null || echo "  (no backups found)"
  exit 1
fi

echo "=== kv-dockerized restore ==="
echo "  Snapshot: $SNAPSHOT"

# Confirm
if [ "$YES" = false ]; then
  echo ""
  echo "WARNING: This will stop Dragonfly and replace all data."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

# Stop services
echo "Stopping Dragonfly..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" down 2>&1 | tail -1

# Copy snapshot into primary data volume
echo "Copying snapshot to primary volume..."
if [ -d "$SNAPSHOT" ]; then
  SRC_DIR="$(cd "$SNAPSHOT" && pwd)"
  docker run --rm -v "kv-dockerized_primary_data:/data" -v "$SRC_DIR:/backup:ro" \
    alpine sh -c "cp /backup/*.dfs /data/ && chmod 644 /data/*.dfs" 2>/dev/null
  docker run --rm -v "kv-dockerized_replica_data:/data" -v "$SRC_DIR:/backup:ro" \
    alpine sh -c "cp /backup/*.dfs /data/ && chmod 644 /data/*.dfs" 2>/dev/null
else
  SNAPSHOT_NAME=$(basename "$SNAPSHOT")
  docker run --rm -v "kv-dockerized_primary_data:/data" -v "$(dirname "$(realpath "$SNAPSHOT")"):/backup:ro" \
    alpine sh -c "cp /backup/$SNAPSHOT_NAME /data/dump.dfs && chmod 644 /data/dump.dfs" 2>/dev/null
  docker run --rm -v "kv-dockerized_replica_data:/data" -v "$(dirname "$(realpath "$SNAPSHOT")"):/backup:ro" \
    alpine sh -c "cp /backup/$SNAPSHOT_NAME /data/dump.dfs && chmod 644 /data/dump.dfs" 2>/dev/null
fi
echo "  Volumes ready"

# Start services
echo "Starting Dragonfly (auto-loads snapshot)..."
docker compose -f "$COMPOSE_DIR/docker-compose.yml" up -d 2>&1 | tail -1

# Verify
echo -n "Waiting for Dragonfly..."
until RC PING | grep -q "PONG"; do sleep 2; done
echo ""

# Check restored keys
KEYS=$(RC DBSIZE)
if [ -n "$KEYS" ]; then
  echo "  Keys restored: $KEYS"
else
  echo "  FAIL: no response"
  exit 1
fi

# Re-apply cluster config
cd "$COMPOSE_DIR"
bash init.sh 2>&1 | tail -5

echo ""
echo "=== restore complete ==="
echo "  Verify: redis-cli -p $PRIMARY_PORT -a $DF_PASSWORD DBSIZE"
