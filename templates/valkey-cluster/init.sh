#!/bin/bash
# valkey-cluster init — apply the StatefulSet and bootstrap the native Valkey
# Cluster ({{ .Nodes }} nodes, {{ .ReplicasPerPrimary }} replica per shard).
# Idempotent: skips creation when the cluster is already healthy (all 16384
# slots assigned), so re-provisioning only refreshes the manifest.
set -e

NS="{{ .Namespace }}"
NAME="{{ .Name }}"
NODES="{{ .Nodes }}"
REPLICAS="{{ .ReplicasPerPrimary }}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/valkey-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASSWORD="${VK_PASSWORD:-{{ .Password }}}"

log()  { echo "[valkey-cluster] $1"; }
fail() { echo "[valkey-cluster] FAIL: $1"; exit 1; }

# kubectl exec can occasionally hang (kubelet streaming flake) — every call is
# bounded so the deadline loops can never block on a single exec.
VK() { timeout -k 5 15 $KUBECTL -n "$NS" exec valkey-0 -c valkey -- valkey-cli -a "$PASSWORD" --no-auth-warning "$@" 2>/dev/null; }

log "applying manifest"
$KUBECTL apply -f "$DIR/valkey.yaml" >/dev/null || fail "manifest apply"

log "waiting for $NODES nodes Ready"
deadline=$((SECONDS + 600))
ready=0
while [ "$SECONDS" -lt "$deadline" ]; do
  ready="$($KUBECTL -n "$NS" get pods -l app=valkey --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
  [ "${ready:-0}" -ge "$NODES" ] && break
  sleep 5
done
[ "${ready:-0}" -ge "$NODES" ] || fail "pods not ready ($ready/$NODES)"

state="$(VK cluster info 2>/dev/null | grep -oE 'cluster_state:[a-z]+' | cut -d: -f2 || true)"
if [ "$state" != "ok" ]; then
  # A full-cluster restart changes the pod IPs while nodes.conf still holds the
  # old ones, so the nodes cannot reach each other (state:fail with all slots
  # assigned). Re-introduce them via CLUSTER MEET; the gossip merges back into
  # one config (node IDs/configEpochs/slots are preserved).
  log "cluster not healthy (state:${state:-unknown}) — re-merging nodes by current IP"
  ipmap="$($KUBECTL -n "$NS" get pods -l app=valkey -o jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' 2>/dev/null)"
  for ip in $ipmap; do
    VK cluster meet "$ip" 6379 >/dev/null 2>&1 || true
  done
  deadline=$((SECONDS + 90))
  while [ "$SECONDS" -lt "$deadline" ]; do
    state="$(VK cluster info 2>/dev/null | grep -oE 'cluster_state:[a-z]+' | cut -d: -f2 || true)"
    [ "$state" = "ok" ] && break
    sleep 3
  done
  [ "$state" = "ok" ] && log "cluster re-merged (state:ok)"
fi

if [ "$state" = "ok" ]; then
  log "cluster already formed (state:ok) — nothing to bootstrap"
else
  # A failed/partial bootstrap leaves the nodes knowing each other with zero
  # slots assigned, and --cluster create then refuses ("node is not empty").
  # With no slots there is no topology to preserve: reset every node first.
  slots="$(VK cluster info 2>/dev/null | grep -oE 'cluster_slots_assigned:[0-9]+' | cut -d: -f2 || true)"
  if [ "${slots:-0}" = "0" ]; then
    log "no slots assigned — CLUSTER RESET HARD on all nodes"
    for i in $(seq 0 $((NODES - 1))); do
      timeout -k 5 15 $KUBECTL -n "$NS" exec "valkey-$i" -c valkey -- valkey-cli -a "$PASSWORD" --no-auth-warning cluster reset hard >/dev/null 2>&1 || true
    done
    sleep 3
  fi
  log "creating the cluster ($NODES nodes, $REPLICAS replica(s) per shard)"
  peers=""
  for i in $(seq 0 $((NODES - 1))); do
    peers="$peers valkey-$i.valkey-headless.$NS.svc.cluster.local:6379"
  done
  VK --cluster create $peers --cluster-replicas "$REPLICAS" --cluster-yes >/dev/null || fail "cluster create"
fi

deadline=$((SECONDS + 120))
slots=0
while [ "$SECONDS" -lt "$deadline" ]; do
  slots="$(VK cluster info 2>/dev/null | grep -oE 'cluster_slots_assigned:[0-9]+' | cut -d: -f2 || true)"
  [ "${slots:-0}" = "16384" ] && break
  sleep 3
done
[ "${slots:-0}" = "16384" ] || fail "slots not fully assigned (${slots:-0}/16384)"

masters="$(VK cluster nodes 2>/dev/null | awk '$3 ~ /master/ {c++} END {print c+0}')"
log "cluster up: ${masters} primaries, 16384/16384 slots"
log "clients: valkey-cli -c -h $NAME.$NS.svc.cluster.local -p 6379 -a <password>"
