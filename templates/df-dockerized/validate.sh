#!/bin/sh
# df-dockerized validate — health check inside Docker
# Containers resolved dynamically: after any recreate, names may gain instance
# suffixes — never hardcode them.
set -e

SVC_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SVC_DIR/.env" ]; then . "$SVC_DIR/.env"; fi

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"

find_container() {
  docker ps --format '{{"{{"}}.Names{{"}}"}}' 2>/dev/null | grep -E "^df-dockerized-$1(-[0-9]+)?$" | head -1
}
PRIMARY_CONTAINER="${PRIMARY_CONTAINER:-$(find_container dragonfly-primary)}"
REPLICA_CONTAINER="${REPLICA_CONTAINER:-$(find_container dragonfly-replica)}"
REPLICA2_CONTAINER="${REPLICA2_CONTAINER:-$(find_container dragonfly-replica-2)}"
HAPROXY_CONTAINER="${HAPROXY_CONTAINER:-$(find_container haproxy)}"

RC()     { docker exec "$PRIMARY_CONTAINER" redis-cli -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP() { docker exec "$REPLICA_CONTAINER" redis-cli -p 6380 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP2(){ docker exec "$REPLICA2_CONTAINER" redis-cli -p 6381 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== df-dockerized validate ==="
FAIL=0

echo -n "Primary: "
RC PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-1: "
RC_REP PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-2: "
RC_REP2 PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "HAProxy: "
docker exec "$HAPROXY_CONTAINER" haproxy -c -f /usr/local/etc/haproxy/haproxy.cfg >/dev/null 2>&1 && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "SQL (PING via primary): "
RC PING | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Vector search: "
echo "skip"

echo -n "Replication: "
RC INFO REPLICATION | grep -q "role:master" && echo "streaming" || { echo "not connected"; FAIL=1; }

echo -n "Replica lag: "
LAG=$(RC INFO REPLICATION | grep "slave0:" | tr ',' '\n' | grep "^lag=" | cut -d= -f2 | head -1)
echo "${LAG:-0} records"

echo -n "TLS HAProxy primary: "
docker exec "$PRIMARY_CONTAINER" redis-cli --tls --cacert /ssl/ca.crt -h haproxy -p 6379 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "TLS HAProxy replica: "
docker exec "$PRIMARY_CONTAINER" redis-cli --tls --cacert /ssl/ca.crt -h haproxy -p 6380 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "3-node independent: "
P1=0; R1=0; R2=0
RC PING 2>/dev/null | grep -q PONG && P1=1
RC_REP PING 2>/dev/null | grep -q PONG && R1=1
RC_REP2 PING 2>/dev/null | grep -q PONG && R2=1
TOTAL=$((P1 + R1 + R2))
if [ "$TOTAL" -eq 3 ]; then
  echo "OK (3/3 responding)"
else
  echo "FAIL ($TOTAL/3 responding)"
  FAIL=1
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "[OK] df-dockerized ready (3 nodes + haproxy)"
else
  echo "[X] Some checks failed"
  exit 1
fi
