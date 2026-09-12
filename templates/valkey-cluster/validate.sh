#!/bin/bash
# valkey-cluster validate — pods up, cluster_state ok, all 16384 slots
# assigned, 3 primaries, PING through the ClusterIP seed service.
set -u

NS="${VK_K8S_NAMESPACE:-valkey}"
NAME="${VK_K8S_NAME:-valkey}"
WANT="${VK_K8S_NODES:-6}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/valkey-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASSWORD="${VK_PASSWORD:-valkey}"
RCI="valkey/valkey:{{ .Tag }}"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }
VK() { $KUBECTL -n "$NS" exec valkey-0 -c valkey -- valkey-cli -a "$PASSWORD" --no-auth-warning "$@" 2>/dev/null; }

echo "=== valkey-cluster validate (ns=$NS name=$NAME) ==="

running="$($KUBECTL -n "$NS" get pods -l app=valkey --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l | tr -d ' ')"
[ "${running:-0}" -ge "$WANT" ] && ok "pods running $running/$WANT" || bad "pods running ${running:-0}/$WANT"

cinfo="$(VK cluster info || true)"
state="$(echo "$cinfo" | grep -oE 'cluster_state:[a-z]+' | cut -d: -f2)"
[ "$state" = "ok" ] && ok "cluster_state: ok" || bad "cluster_state: ${state:-unknown}"

slots="$(echo "$cinfo" | grep -oE 'cluster_slots_assigned:[0-9]+' | cut -d: -f2)"
[ "${slots:-0}" = "16384" ] && ok "slots assigned: 16384/16384" || bad "slots assigned: ${slots:-0}/16384"

masters="$(VK cluster nodes 2>/dev/null | awk '$3 ~ /master/ {c++} END {print c+0}')"
[ "${masters:-0}" -eq 3 ] && ok "primaries: 3" || bad "primaries: ${masters:-0} (want 3)"

$KUBECTL -n "$NS" delete pod vk-validate --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
if $KUBECTL -n "$NS" run vk-validate --rm -i --restart=Never --image="$RCI" --command -- \
     valkey-cli -c -h "$NAME.$NS.svc" -p 6379 -a "$PASSWORD" --no-auth-warning PING 2>/dev/null | grep -q PONG; then
  ok "PING via $NAME.$NS.svc:6379 (seed service)"
else
  bad "PING via seed service"
fi
$KUBECTL -n "$NS" delete pod vk-validate --force --grace-period=0 --ignore-not-found >/dev/null 2>&1

if [ "$FAILED" -eq 0 ]; then echo "=== valkey-cluster validate PASSED ==="; else echo "=== valkey-cluster validate FAILED ==="; fi
exit "$FAILED"
