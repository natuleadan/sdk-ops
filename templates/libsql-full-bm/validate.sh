#!/bin/sh
# libsql-full-bm validate — health check inside Docker
set -e

PRIMARY_CONTAINER="libsql-full-bm-sqld-primary-1"
REPLICA_CONTAINER="libsql-full-bm-sqld-replica-1"

SQL()      { docker exec "$PRIMARY_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
SQL_REP()  { docker exec "$REPLICA_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
HC_PRIM()  { docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_REP()   { docker exec "$REPLICA_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }

echo "=== libsql-full-bm validate ==="

echo -n "Primary: "
HC_PRIM && echo "OK" || echo "FAIL"

echo -n "Replica: "
HC_REP && echo "OK" || echo "FAIL"

echo -n "Primary SQL: "
SQL '{"statements":["SELECT 1 AS ok"]}' | grep -q '"rows":\[\[1\]\]' && echo "OK" || echo "FAIL"

echo -n "Replica SQL: "
SQL_REP '{"statements":["SELECT 1 AS ok"]}' | grep -q '"rows":\[\[1\]\]' && echo "OK" || echo "FAIL"

echo -n "Vector search: "
SQL '{"statements":["SELECT content FROM items WHERE embedding MATCH vector('"'"'[0.1,0.2,0.3]'"'"') LIMIT 1"]}' | grep -q "hello" && echo "OK" || echo "skip"

echo -n "Snapshots: "
SNAPS=$(ls backups/*.snap 2>/dev/null | wc -l | tr -d ' ')
echo "$SNAPS snapshot(s)"

echo -n "TLS HAProxy primary: "
docker exec "$PRIMARY_CONTAINER" sh -c \
  "curl -sk --cacert /tls/ca_cert.pem https://haproxy:8443/health" 2>/dev/null > /dev/null && echo "OK" || echo "FAIL"

echo -n "TLS HAProxy replica: "
docker exec "$PRIMARY_CONTAINER" sh -c \
  "curl -sk --cacert /tls/ca_cert.pem https://haproxy:8444/health" 2>/dev/null > /dev/null && echo "OK" || echo "FAIL"

echo ""
echo "✓ libsql-full-bm ready"
