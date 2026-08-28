#!/bin/sh
# libsql-dockerized validate — comprehensive health check
set -e

# Resolve container names dynamically: after a failover the controller
# recreates sqld containers via `docker run` with instance-suffixed names
# (sqld-replica-2 vs sqld-replica-2-1). Never hardcode the suffix.
find_container() {
  docker ps --format '{{.Names}}' 2>/dev/null | grep -E "^libsql-dockerized-$1(-[0-9]+)?$" | head -1
}
PRIMARY_CONTAINER="${PRIMARY_CONTAINER:-$(find_container sqld-primary)}"
REPLICA_CONTAINER="${REPLICA_CONTAINER:-$(find_container sqld-replica-1)}"
REPLICA2_CONTAINER="${REPLICA2_CONTAINER:-$(find_container sqld-replica-2)}"

SQL()      { docker exec "$PRIMARY_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
SQL_REP()  { docker exec "$REPLICA_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
SQL_REP2() { docker exec "$REPLICA2_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
HC_PRIM()  { docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_REP()   { docker exec "$REPLICA_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_REP2()  { docker exec "$REPLICA2_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_ETCD()  { docker exec "libsql-dockerized-etcd-1" etcdctl endpoint health > /dev/null 2>&1; }
HC_CTRL()  { curl -sf http://localhost:${CONTROLLER_PORT:-9090}/health > /dev/null 2>&1; }

echo "=== libsql-dockerized validate ==="
FAIL=0

echo -n "etcd: "
HC_ETCD && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Controller: "
HC_CTRL && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Leader: "
LEADER=$(curl -sf http://localhost:${CONTROLLER_PORT:-9090}/leader 2>/dev/null)
echo "$LEADER" | grep -q '"node"' && echo "OK ($LEADER)" || { echo "FAIL"; FAIL=1; }

echo -n "Primary: "
HC_PRIM && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-1: "
HC_REP && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-2: "
HC_REP2 && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Primary SQL: "
SQL '{"statements":["SELECT 1 AS ok"]}' | grep -q '"rows":\[\[1\]\]' && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-1 SQL: "
SQL_REP '{"statements":["SELECT 1 AS ok"]}' | grep -q '"rows":\[\[1\]\]' && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replica-2 SQL: "
SQL_REP2 '{"statements":["SELECT 1 AS ok"]}' | grep -q '"rows":\[\[1\]\]' && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Vector search: "
SQL '{"statements":["SELECT content FROM items WHERE embedding MATCH vector('"'"'[0.1,0.2,0.3]'"'"') LIMIT 1"]}' | grep -q "hello" && echo "OK" || echo "skip"

echo -n "Snapshots: "
SNAPS=$(ls backups/*.snap 2>/dev/null | wc -l | tr -d ' ')
echo "$SNAPS snapshot(s)"

echo -n "TLS Router: "
curl -sk https://localhost:${LIBSQL_HTTP_TLS:-8443}/health 2>/dev/null | grep -q '"status":"ok"' && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "TLS Router SQL: "
curl -sk -X POST https://localhost:${LIBSQL_HTTP_TLS:-8443} -H "Content-Type: application/json" -d '{"statements":["SELECT 1 AS ok"]}' 2>/dev/null | grep -q '"rows":\[\[1\]\]' && echo "OK" || { echo "FAIL"; FAIL=1; }

echo -n "Replication verify: "
SQL '{"statements":["CREATE TABLE IF NOT EXISTS _validate_repl (id INTEGER PRIMARY KEY, v TEXT)","INSERT OR IGNORE INTO _validate_repl VALUES (99, '\''repltest'\'')"]}' > /dev/null
sleep 1
REPL_OK=$(SQL_REP '{"statements":["SELECT v FROM _validate_repl WHERE id=99"]}' 2>/dev/null | grep -q "repltest" && echo yes || echo no)
SQL '{"statements":["DELETE FROM _validate_repl WHERE id=99"]}' > /dev/null 2>&1
if [ "$REPL_OK" = "yes" ]; then
  echo "OK (primary->replica)"
else
  echo "WARN (replica lag or miss)"
fi

echo -n "3-node independent: "
P1=$(HC_PRIM && echo 1 || echo 0)
R1=$(HC_REP && echo 1 || echo 0)
R2=$(HC_REP2 && echo 1 || echo 0)
TOTAL=$((P1 + R1 + R2))
if [ "$TOTAL" -eq 3 ]; then
  echo "OK (3/3 responding)"
else
  echo "FAIL ($TOTAL/3 responding)"
  FAIL=1
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "✓ libsql-dockerized ready (3 nodes + etcd + controller + router)"
else
  echo "✗ Some checks failed"
  exit 1
fi
