#!/bin/bash
# valkey-cluster test — sharding across the 3 primaries, real failover (delete
# a primary -> its replica is promoted), data survivorship, cleanup.
set -u

NS="${VK_K8S_NAMESPACE:-valkey}"
NAME="${VK_K8S_NAME:-valkey}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/valkey-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASSWORD="${VK_PASSWORD:-valkey}"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }
CLI() { $KUBECTL -n "$NS" exec valkey-0 -c valkey -- valkey-cli -c -a "$PASSWORD" --no-auth-warning "$@" 2>/dev/null; }
POD() { $KUBECTL -n "$NS" exec "$1" -c valkey -- valkey-cli -a "$PASSWORD" --no-auth-warning "${@:2}" 2>/dev/null; }

keys=30

echo "=== valkey-cluster test ==="

# Master map + baseline sizes, captured BEFORE the test writes (deltas below).
declare -A before_dbsize
master_pods=""
for i in $(seq 0 5); do
  role="$(POD "valkey-$i" INFO replication | grep -oE 'role:(master|slave)' | cut -d: -f2)"
  if [ "$role" = "master" ]; then
    master_pods="$master_pods valkey-$i"
    before_dbsize["valkey-$i"]="$(POD "valkey-$i" DBSIZE)"
  fi
done

echo "-- step 1: write $keys sharded keys --"
for i in $(seq 1 $keys); do CLI SET "sdkops:t:$i" "v$i" >/dev/null; done
n=0
for i in $(seq 1 $keys); do [ "$(CLI GET "sdkops:t:$i")" = "v$i" ] && n=$((n+1)); done
[ "$n" -eq "$keys" ] && ok "write/read $n/$keys keys (cluster-aware client)" || bad "write/read only $n/$keys"

echo "-- step 2: sharding across primaries --"
echo "    primaries:$master_pods"
count_masters=$(echo $master_pods | wc -w | tr -d ' ')
[ "$count_masters" -eq 3 ] && ok "3 primaries" || bad "primaries: $count_masters (want 3)"

# Deltas, not absolute sizes: the cluster may legitimately hold other data.
total_delta=0; with_delta=0
for p in $master_pods; do
  d="$(POD "$p" DBSIZE)"
  delta=$(( ${d:-0} - ${before_dbsize[$p]:-0} ))
  total_delta=$((total_delta + delta))
  [ "$delta" -gt 0 ] && with_delta=$((with_delta + 1))
done
[ "$total_delta" -eq "$keys" ] && ok "keys distributed: +$total_delta" || bad "DBSIZE delta +$total_delta (want +$keys)"
[ "$with_delta" -eq 3 ] && ok "all 3 shards received data" || bad "only $with_delta shard(s) received data"

echo "-- step 3: failover (delete a primary) --"
victim=$(echo $master_pods | awk '{print $1}')
echo "    deleting $victim — k8s restarts it; the cluster promotes its replica"
$KUBECTL -n "$NS" delete pod "$victim" --wait=false >/dev/null 2>&1

deadline=$((SECONDS + 180))
state=""; m=0
while [ "$SECONDS" -lt "$deadline" ]; do
  state="$(CLI cluster info | grep -oE 'cluster_state:[a-z]+' | cut -d: -f2)"
  m="$(CLI cluster nodes | awk '$3 ~ /master/ {c++} END {print c+0}')"
  [ "$state" = "ok" ] && [ "$m" -eq 3 ] && break
  sleep 5
done
[ "$state" = "ok" ] && [ "$m" -eq 3 ] && ok "cluster recovered: state=ok, 3 primaries (promotion happened)" || bad "cluster not recovered (state=${state:-?} masters=${m:-?})"

$KUBECTL -n "$NS" wait --for=condition=Ready "pod/$victim" --timeout=120s >/dev/null 2>&1 && ok "$victim rejoined the cluster" || bad "$victim did not rejoin"

echo "-- step 4: data survived the failover --"
n=0
for i in $(seq 1 $keys); do [ "$(CLI GET "sdkops:t:$i")" = "v$i" ] && n=$((n+1)); done
[ "$n" -eq "$keys" ] && ok "all $keys keys survived" || bad "keys lost: only $n/$keys survived"

echo "-- step 5: cleanup --"
for i in $(seq 1 $keys); do CLI DEL "sdkops:t:$i" >/dev/null; done
ok "cleaned"

if [ "$FAILED" -eq 0 ]; then echo "=== valkey-cluster test PASSED ==="; else echo "=== valkey-cluster test FAILED ==="; fi
exit "$FAILED"
