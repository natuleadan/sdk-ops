#!/bin/bash
# etcd validate — local member health (run by the 5-min timer).
set -u
docker exec etcd-shared-etcd-1 etcdctl --endpoints=127.0.0.1:2379 --command-timeout=6s endpoint health 2>/dev/null | grep -q healthy \
  || { echo "FAIL: etcd local member unhealthy"; exit 1; }
echo "OK: etcd member healthy"
