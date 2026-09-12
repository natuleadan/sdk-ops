#!/bin/bash
# df-cluster validate — pods ready, CR state, master PING via the service,
# replication wiring. Runs on the node. Exit code real.
set -u

NS="${DF_K8S_NAMESPACE:-df}"
NAME="${DF_K8S_NAME:-df}"
WANT="${DF_K8S_REPLICAS:-3}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/df-cluster"
[ -f "$DIR/.env" ] && . "$DIR/.env"
PASS="${DF_PASSWORD:-dragonfly}"
RCI="${RCI_IMAGE:-redis:7.0.10}"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }

echo "=== df-cluster validate (ns=$NS name=$NAME) ==="

# 1. The CR exists and pods are Running.
cr="$($KUBECTL -n "$NS" get dragonfly "$NAME" -o name 2>/dev/null)"
[ -n "$cr" ] && ok "Dragonfly CR $NAME" || bad "Dragonfly CR $NAME"
running="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --field-selector=status.phase=Running -o name 2>/dev/null | wc -l | tr -d ' ')"
[ "${running:-0}" -ge "$WANT" ] && ok "pods running $running/$WANT" || bad "pods running ${running:-0}/$WANT"

# 2. Master PING through the operator service (points at the master).
$KUBECTL -n "$NS" delete pod df-validate-cli --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
$KUBECTL -n "$NS" run df-validate-cli --restart=Never --image="$RCI" --command -- redis-cli -h "$NAME.$NS.svc" -p 6379 -a "$PASS" --no-auth-warning PING >/dev/null 2>&1
# Wait for the pod to complete (max 30s).
phase=""
for i in $(seq 1 15); do
  phase="$($KUBECTL -n "$NS" get pod df-validate-cli -o jsonpath='{.status.phase}' 2>/dev/null)"
  [ "$phase" = "Succeeded" ] && break
  [ "$phase" = "Failed" ] && break
  sleep 2
done
if [ "$phase" = "Succeeded" ] && $KUBECTL -n "$NS" logs df-validate-cli 2>/dev/null | grep -q PONG; then
  ok "master PING via $NAME.$NS.svc:6379"
else
  bad "master PING via service (phase=${phase:-unknown})"
fi
$KUBECTL -n "$NS" delete pod df-validate-cli --force --grace-period=0 --ignore-not-found >/dev/null 2>&1

# 3. Replication: the role + connected replicas from the master's INFO.
# (run + logs instead of `run --rm -i`: the stdin attach races the pod start.)
$KUBECTL -n "$NS" delete pod df-validate-rep --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
$KUBECTL -n "$NS" run df-validate-rep --restart=Never --image="$RCI" --command -- redis-cli -h "$NAME.$NS.svc" -p 6379 -a "$PASS" --no-auth-warning INFO replication >/dev/null 2>&1
phase=""
for i in $(seq 1 15); do
  phase="$($KUBECTL -n "$NS" get pod df-validate-rep -o jsonpath='{.status.phase}' 2>/dev/null)"
  [ "$phase" = "Succeeded" ] && break
  [ "$phase" = "Failed" ] && break
  sleep 2
done
info="$($KUBECTL -n "$NS" logs df-validate-rep 2>/dev/null || true)"
$KUBECTL -n "$NS" delete pod df-validate-rep --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
if echo "$info" | grep -q "role:master"; then
  ok "service points at a master"
  slaves="$(echo "$info" | grep -oE "connected_slaves:[0-9]+" | cut -d: -f2 | tr -d '[:space:]')"
  [ "${slaves:-0}" -ge "$((WANT - 1))" ] && ok "replicas connected: ${slaves:-0}" || bad "replicas connected: ${slaves:-0}"
else
  bad "INFO replication (no master role, phase=${phase:-unknown})"
fi

if [ "$FAILED" -eq 0 ]; then echo "=== df-cluster validate PASSED ==="; else echo "=== df-cluster validate FAILED ==="; fi
exit "$FAILED"
