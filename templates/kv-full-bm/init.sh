#!/bin/sh
# kv-full-bm init — SSL, services, cluster config
# All redis commands inside Docker via docker exec
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_HOST="${PRIMARY_HOST:-dragonfly-primary}"
REPLICA_HOST="${REPLICA_HOST:-dragonfly-replica}"
PRIMARY_PORT="${PRIMARY_PORT:-6379}"
REPLICA_PORT="${REPLICA_PORT:-6380}"
PRIMARY_ADMIN="${PRIMARY_ADMIN:-10001}"
REPLICA_ADMIN="${REPLICA_ADMIN:-10002}"
PRIMARY_CONTAINER="kv-full-bm-dragonfly-primary-1"
REPLICA_CONTAINER="kv-full-bm-dragonfly-replica-1"

# Data operations (no TLS, internal network)
RC()     { docker exec "$PRIMARY_CONTAINER" redis-cli -p "$PRIMARY_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP() { docker exec "$REPLICA_CONTAINER" redis-cli -p "$REPLICA_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }

# Admin operations (admin ports, no TLS)
RC_ADMIN()     { docker exec "$PRIMARY_CONTAINER" redis-cli -p "$PRIMARY_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN_REP() { docker exec "$REPLICA_CONTAINER" redis-cli -p "$REPLICA_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== kv-full-bm init ==="
echo "Password: $DF_PASSWORD"

# Cross-platform sed helper
_ni() { sed -i.bak "$1" "$2" && rm -f "${2}.bak"; }

# Generate SSL certificates
mkdir -p ssl backups

if [ ! -f ssl/server.key ]; then
  bash gen-certs.sh
fi

# Start services
echo "Starting Dragonfly..."
docker compose up -d 2>&1 | tail -1

# Wait for primary
echo -n "Waiting for primary..."
until RC PING | grep -q "PONG"; do sleep 2; done
echo " OK"

# Wait for replica
echo -n "Waiting for replica..."
until RC_REP PING | grep -q "PONG"; do sleep 2; done
echo " OK"

# Configure replication
echo "Configuring replication..."
RC_ADMIN_REP REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT"
echo "  REPLICAOF configured"

# Configure cluster
echo "Configuring cluster..."
MASTER_ID=$(RC_ADMIN CLUSTER MYID)
REPLICA_ID=$(RC_ADMIN_REP CLUSTER MYID)

if [ -n "$MASTER_ID" ] && [ -n "$REPLICA_ID" ]; then
  CONFIG="[{\"slot_ranges\":[{\"start\":0,\"end\":16383}],\"master\":{\"id\":\"${MASTER_ID}\",\"ip\":\"${PRIMARY_HOST}\",\"port\":${PRIMARY_PORT}},\"replicas\":[{\"id\":\"${REPLICA_ID}\",\"ip\":\"${REPLICA_HOST}\",\"port\":${REPLICA_PORT}}]}]"
  RC_ADMIN DFLYCLUSTER CONFIG "$CONFIG"
  RC_ADMIN_REP DFLYCLUSTER CONFIG "$CONFIG"
  echo "  Cluster configured"
else
  echo "  WARN: could not get node IDs"
fi

echo ""
echo "✓ kv-full-bm ready"
echo "  Backup: bash backup.sh"
echo "  Restore: bash restore.sh --help"
