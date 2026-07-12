#!/bin/sh
# kv-dockerized init — SSL, services, cluster config
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_HOST="${PRIMARY_HOST:-dragonfly-primary}"
REPLICA_HOST="${REPLICA_HOST:-dragonfly-replica}"
REPLICA2_HOST="${REPLICA2_HOST:-dragonfly-replica-2}"
PRIMARY_PORT="${PRIMARY_PORT:-6379}"
REPLICA_PORT="${REPLICA_PORT:-6380}"
REPLICA2_PORT="${REPLICA2_PORT:-6381}"
PRIMARY_ADMIN="${PRIMARY_ADMIN:-10001}"
REPLICA_ADMIN="${REPLICA_ADMIN:-10002}"
REPLICA2_ADMIN="${REPLICA2_ADMIN:-10003}"
PRIMARY_CONTAINER="kv-dockerized-dragonfly-primary-1"
REPLICA_CONTAINER="kv-dockerized-dragonfly-replica-1"
REPLICA2_CONTAINER="kv-dockerized-dragonfly-replica-2-1"

RC()         { docker exec "$PRIMARY_CONTAINER" redis-cli -p "$PRIMARY_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP()     { docker exec "$REPLICA_CONTAINER" redis-cli -p "$REPLICA_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP2()    { docker exec "$REPLICA2_CONTAINER" redis-cli -p "$REPLICA2_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN()   { docker exec "$PRIMARY_CONTAINER" redis-cli -p "$PRIMARY_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN_REP()  { docker exec "$REPLICA_CONTAINER" redis-cli -p "$REPLICA_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN_REP2() { docker exec "$REPLICA2_CONTAINER" redis-cli -p "$REPLICA2_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== kv-dockerized init ==="
echo "Password: $DF_PASSWORD"

mkdir -p ssl backups
if [ ! -f ssl/server.key ]; then
  bash gen-certs.sh
fi

echo "Starting Dragonfly..."
docker compose up -d 2>&1 | tail -1

echo -n "Waiting for primary..."
until RC PING | grep -q "PONG"; do sleep 2; done
echo " OK"

echo -n "Waiting for replica-1..."
until RC_REP PING | grep -q "PONG"; do sleep 2; done
echo " OK"

echo -n "Waiting for replica-2..."
until RC_REP2 PING | grep -q "PONG"; do sleep 2; done
echo " OK"

echo "Configuring replication..."
RC_ADMIN_REP REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT"
RC_ADMIN_REP2 REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT"
echo "  REPLICAOF configured"

echo "Configuring cluster..."
MASTER_ID=$(RC_ADMIN CLUSTER MYID)
REPLICA_ID=$(RC_ADMIN_REP CLUSTER MYID)
REPLICA2_ID=$(RC_ADMIN_REP2 CLUSTER MYID)

if [ -n "$MASTER_ID" ] && [ -n "$REPLICA_ID" ]; then
  CONFIG="[{\"slot_ranges\":[{\"start\":0,\"end\":16383}],\"master\":{\"id\":\"${MASTER_ID}\",\"ip\":\"${PRIMARY_HOST}\",\"port\":${PRIMARY_PORT}},\"replicas\":[{\"id\":\"${REPLICA_ID}\",\"ip\":\"${REPLICA_HOST}\",\"port\":${REPLICA_PORT}},{\"id\":\"${REPLICA2_ID}\",\"ip\":\"${REPLICA2_HOST}\",\"port\":${REPLICA2_PORT}}]}]"
  RC_ADMIN DFLYCLUSTER CONFIG "$CONFIG"
  RC_ADMIN_REP DFLYCLUSTER CONFIG "$CONFIG"
  RC_ADMIN_REP2 DFLYCLUSTER CONFIG "$CONFIG"
  echo "  Cluster configured"
else
  echo "  WARN: could not get node IDs"
fi

echo ""
echo "✓ kv-dockerized ready"
echo "  Backup: bash backup.sh"
echo "  Restore: bash restore.sh --help"
