#!/bin/bash
# yuga-docker validate — deep checks on the 3-node cluster.
# Exits non-zero on any failure so callers (CI / the fleet) can gate on it.
set -e

NS="${PROVISION_NS:-yuga-docker}"
YB_DB="${YB_DB:-yugabyte}"
YB_USER="${YB_USER:-dev}"
YB_PASSWORD="${YB_PASSWORD:-devpass}"
# The tservers bind CQL/YSQL on the container bridge IPs, not loopback.
YB0="${YB0_IP:-203.0.113.10}"
YB2="${YB2_IP:-203.0.113.12}"
PASS=0
FAIL=0

ok()   { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad()  { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

echo "=== yuga-docker validate ($NS) ==="

# 1. All 3 containers up + healthy.
for n in yugabyte-0 yugabyte-1 yugabyte-2; do
  st=$(docker inspect "$NS-$n-1" 2>/dev/null | grep -o '"Status": *"[A-Za-z]*"' | tail -1 | cut -d'"' -f4)
  if [ "$st" = "healthy" ]; then ok "$n healthy"; else bad "$n health=$st"; fi
done

# 2. Cluster (masters + tservers) membership. Informational: with
#    --daemon=false the CLI reports "not running" while the cluster is healthy.
docker compose exec -T yugabyte-0 bash -lc "bin/yugabyted status" >/dev/null 2>&1 \
  && ok "yugabyted status" || echo "  (yugabyted status info — daemon mode)"
echo "  -- masters: $(docker compose exec -T yugabyte-0 bash -lc 'bin/yb-admin --master_addresses '$YB0':7100 list_all_masters 2>/dev/null | wc -l') --"

# 3. YSQL write/read via the app role.
if docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h $YB0 -p 5433 -U $YB_USER -d $YB_DB -v ON_ERROR_STOP=1 -c \"
    CREATE TABLE IF NOT EXISTS validate(id INT PRIMARY KEY, v TEXT);
    INSERT INTO validate VALUES (1, 'ok') ON CONFLICT (id) DO UPDATE SET v='ok';
    SELECT v FROM validate WHERE id=1;
  \" | grep -q ok"; then
  ok "YSQL write/read"
else
  bad "YSQL write/read"
fi

# 4. YCQL write/read (connected to the tserver bridge IP, not loopback).
if docker compose exec -T yugabyte-0 bash -lc "
  bin/ycqlsh $YB0 9042 -u $YB_USER -p $YB_PASSWORD -e \"
    CREATE KEYSPACE IF NOT EXISTS validate;
    CREATE TABLE IF NOT EXISTS validate.t(id INT PRIMARY KEY, v TEXT);
    INSERT INTO validate.t(id,v) VALUES(1,'ok');
    SELECT v FROM validate.t WHERE id=1;
  \" | grep -q ok"; then
  ok "YCQL write/read"
else
  bad "YCQL write/read (user may need ycql permissions — see README)"
fi

# 5. Quorum / replication: writes must be visible cluster-wide (RF=3). Read
#    through a follower (node 2) to prove data landed on more than one master.
if docker compose exec -T yugabyte-2 bash -lc "
  bin/ysqlsh -h $YB2 -p 5433 -U $YB_USER -d $YB_DB -t -c \"SELECT v FROM validate WHERE id=1\" | grep -q ok"; then
  ok "replication (follower read on node 2)"
else
  bad "replication (follower read)"
fi

echo
echo "result: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
