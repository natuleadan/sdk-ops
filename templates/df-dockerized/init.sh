#!/bin/sh
# df-dockerized init — SSL, services, cluster config
set -e

# Secrets written by the provision into .env (0600). Scripts run over SSH with
# a bare environment (sudo strips it), so read them from the file next to the
# script instead of relying on inherited variables.
SVC_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SVC_DIR/.env" ]; then . "$SVC_DIR/.env"; fi

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
PRIMARY_CONTAINER="df-dockerized-dragonfly-primary-1"
REPLICA_CONTAINER="df-dockerized-dragonfly-replica-1"
REPLICA2_CONTAINER="df-dockerized-dragonfly-replica-2-1"

RC()         { docker exec "$PRIMARY_CONTAINER" redis-cli -p "$PRIMARY_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP()     { docker exec "$REPLICA_CONTAINER" redis-cli -p "$REPLICA_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP2()    { docker exec "$REPLICA2_CONTAINER" redis-cli -p "$REPLICA2_PORT" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN()   { docker exec "$PRIMARY_CONTAINER" redis-cli -p "$PRIMARY_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN_REP()  { docker exec "$REPLICA_CONTAINER" redis-cli -p "$REPLICA_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_ADMIN_REP2() { docker exec "$REPLICA2_CONTAINER" redis-cli -p "$REPLICA2_ADMIN" -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== df-dockerized init ==="
echo "Password: $DF_PASSWORD"

mkdir -p ssl backups
if [ ! -f ssl/server.key ]; then
  bash gen-certs.sh
fi
# Files bind-mounted into the haproxy container (config + TLS cert) must be
# readable by its in-container user — the default 0600 deploy perms block it.
chmod 0644 haproxy.cfg ssl/server.pem ssl/ca.crt 2>/dev/null || true

echo "Starting Dragonfly..."
docker compose up -d 2>&1 | tail -1

echo -n "Waiting for primary..."
tries=0
until RC PING 2>/dev/null | grep -q "PONG"; do
  tries=$((tries + 1))
  if [ "$tries" -ge 60 ]; then echo " FAIL: primary never answered (password? TLS?)"; exit 1; fi
  sleep 2
done
echo " OK"

echo -n "Waiting for replica-1..."
tries=0
until RC_REP PING 2>/dev/null | grep -q "PONG"; do
  tries=$((tries + 1))
  if [ "$tries" -ge 60 ]; then echo " FAIL: replica-1 never answered (password? TLS?)"; exit 1; fi
  sleep 2
done
echo " OK"

echo -n "Waiting for replica-2..."
tries=0
until RC_REP2 PING 2>/dev/null | grep -q "PONG"; do
  tries=$((tries + 1))
  if [ "$tries" -ge 60 ]; then echo " FAIL: replica-2 never answered (password? TLS?)"; exit 1; fi
  sleep 2
done
echo " OK"

echo "Configuring replication..."
# Legacy replication (no cluster_mode): REPLICAOF on the admin port. In
# --cluster_mode=yes emulated mode replicas redirect every read to the
# master, which would kill the HAProxy read/write split — plain replication
# keeps the read-split (and INFO replication checks) meaningful.
RC_ADMIN_REP REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT"
RC_ADMIN_REP2 REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT"
sleep 3
RC_ADMIN_REP REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT" 2>/dev/null || true
RC_ADMIN_REP2 REPLICAOF "$PRIMARY_HOST" "$PRIMARY_PORT" 2>/dev/null || true
echo "  REPLICAOF configured"

echo ""
echo "✓ df-dockerized ready"
echo "  Backup: bash backup.sh"
echo "  Restore: bash restore.sh --help"
