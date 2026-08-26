#!/bin/bash
# yuga-docker integration tests: write/read, replication, failover,
# and a backup/restore cycle. Run on the VPS after init.
set -e

NS="${PROVISION_NS:-yuga-docker}"
YB_DB="${YB_DB:-yugabyte}"
YB_USER="${YB_USER:-dev}"
YB_PASSWORD="${YB_PASSWORD:-devpass}"

echo "=== yuga-docker test ==="

# 1. Basic YSQL write/read.
echo "-- test 1: YSQL write/read --"
docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U $YB_USER -d $YB_DB -v ON_ERROR_STOP=1 -c \"
    CREATE TABLE IF NOT EXISTS it(id INT PRIMARY KEY, v TEXT);
    INSERT INTO it VALUES (1, 'hello') ON CONFLICT (id) DO UPDATE SET v='hello';
    SELECT v FROM it WHERE id=1;
  \" | grep -q hello
"

# 2. Replication: write on node 0, read on node 2 (follower).
echo "-- test 2: replication (follower read) --"
docker compose exec -T yugabyte-2 bash -lc "
  bin/ysqlsh -h yugabyte-2 -p 5433 -U $YB_USER -d $YB_DB -t -c 'SELECT v FROM it WHERE id=1'
" | grep -q hello

# 3. Failover: kill node 1 (a tserver/master) and confirm the cluster still
#    serves writes+reads (RF=3 tolerates one node loss).
echo "-- test 3: failover (kill node 1) --"
docker compose stop yugabyte-1
sleep 15
docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U $YB_USER -d $YB_DB -v ON_ERROR_STOP=1 -c \"
    INSERT INTO it VALUES (2, 'after-failover') ON CONFLICT (id) DO UPDATE SET v='after-failover';
  \"
" | grep -q after-failover
docker compose start yugabyte-1
sleep 15

echo "-- test 4: backup/restore cycle --"
# Insert a marker, back it up, then restore and confirm it's back.
docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U $YB_USER -d $YB_DB -v ON_ERROR_STOP=1 -c \"
    INSERT INTO it VALUES (99, 'backup-marker') ON CONFLICT (id) DO UPDATE SET v='backup-marker';
  \"
"
bash backup.sh
docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U yugabyte -v ON_ERROR_STOP=1 -c 'DROP DATABASE $YB_DB'
"
bash restore.sh -y
docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U $YB_USER -d $YB_DB -t -c 'SELECT v FROM it WHERE id=99'
" | grep -q backup-marker

echo "=== ALL TESTS PASSED ==="
