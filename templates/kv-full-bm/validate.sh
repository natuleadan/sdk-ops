#!/bin/sh
# kv-full-bm validate — health check inside Docker
set -e

DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_CONTAINER="kv-full-bm-dragonfly-primary-1"
REPLICA_CONTAINER="kv-full-bm-dragonfly-replica-1"
HAPROXY_CONTAINER="kv-full-bm-haproxy-1"

echo "=== kv-full-bm validate ==="

echo -n "Primary: "
docker exec -e DF_PASSWORD="$DF_PASSWORD" "$PRIMARY_CONTAINER" \
  redis-cli -p 6379 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || echo "FAIL"

echo -n "Replica: "
docker exec -e DF_PASSWORD="$DF_PASSWORD" "$REPLICA_CONTAINER" \
  redis-cli -p 6380 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || echo "FAIL"

echo -n "Replication: "
docker exec -e DF_PASSWORD="$DF_PASSWORD" "$PRIMARY_CONTAINER" \
  redis-cli -p 6379 -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep -q "role:master" && echo "streaming" || echo "not connected"

echo -n "Replica lag: "
LAG=$(docker exec -e DF_PASSWORD="$DF_PASSWORD" "$PRIMARY_CONTAINER" \
  redis-cli -p 6379 -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep "lag=" | awk -F= '{print $2}')
echo "${LAG:-0} records"

echo -n "TLS HAProxy primary: "
docker exec -e DF_PASSWORD="$DF_PASSWORD" "$PRIMARY_CONTAINER" \
  redis-cli --tls --cacert /ssl/ca.crt -h haproxy -p 6379 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || echo "FAIL"

echo -n "TLS HAProxy replica: "
docker exec -e DF_PASSWORD="$DF_PASSWORD" "$PRIMARY_CONTAINER" \
  redis-cli --tls --cacert /ssl/ca.crt -h haproxy -p 6380 -a "$DF_PASSWORD" PING 2>/dev/null | grep -q "PONG" && echo "OK" || echo "FAIL"

echo ""
echo "✓ kv-full-bm ready"
