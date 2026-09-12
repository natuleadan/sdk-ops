#!/bin/bash
# pgsql-cnpg validate — CNPG cluster health: phase, ready instances, primary,
# -rw service endpoints and a real psql PING through the service.
set -u

NS="${PG_K8S_NAMESPACE:-pg}"
NAME="${PG_K8S_NAME:-pg}"
WANT="${PG_K8S_INSTANCES:-3}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
RCI="${RCI_IMAGE:-postgres:17-alpine}"

FAILED=0
ok()  { echo "  [OK] $1"; }
bad() { echo "  [FAIL] $1"; FAILED=1; }

echo "=== pgsql-cnpg validate (ns=$NS name=$NAME) ==="

# 1. The CR exists and is healthy.
cr="$($KUBECTL -n "$NS" get cluster "$NAME" -o name 2>/dev/null)"
[ -n "$cr" ] && ok "Cluster CR $NAME" || bad "Cluster CR $NAME"
phase="$($KUBECTL -n "$NS" get cluster "$NAME" -o jsonpath='{.status.phase}' 2>/dev/null)"
[ "$phase" = "Cluster in healthy state" ] && ok "phase: $phase" || bad "phase: ${phase:-unknown}"
ready="$($KUBECTL -n "$NS" get cluster "$NAME" -o jsonpath='{.status.readyInstances}' 2>/dev/null)"
[ "${ready:-0}" -ge "$WANT" ] && ok "instances ready $ready/$WANT" || bad "instances ready ${ready:-0}/$WANT"

# 2. Primary elected + both services resolvable.
primary="$($KUBECTL -n "$NS" get cluster "$NAME" -o jsonpath='{.status.currentPrimary}' 2>/dev/null)"
[ -n "$primary" ] && ok "primary: $primary" || bad "no primary elected"

# 3. Real PING through the read-write service (app user + generated password).
PASS="$(k3s kubectl -n "$NS" get secret "$NAME-app" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)"
if [ -n "$PASS" ]; then
  if $KUBECTL -n "$NS" delete pod pg-validate-cli --force --grace-period=0 --ignore-not-found >/dev/null 2>&1; then :; fi
  $KUBECTL -n "$NS" run pg-validate-cli --restart=Never --image="$RCI" --command -- \
    psql "postgresql://app:$PASS@$NAME-rw.$NS.svc:5432/app" -tAc "SELECT 1" >/dev/null 2>&1 || true
  phase_pod=""
  for i in $(seq 1 15); do
    phase_pod="$($KUBECTL -n "$NS" get pod pg-validate-cli -o jsonpath='{.status.phase}' 2>/dev/null)"
    [ "$phase_pod" = "Succeeded" ] && break
    [ "$phase_pod" = "Failed" ] && break
    sleep 2
  done
  if [ "$phase_pod" = "Succeeded" ] && $KUBECTL -n "$NS" logs pg-validate-cli 2>/dev/null | grep -q '^1$'; then
    ok "psql PING via $NAME-rw.$NS.svc:5432"
  else
    bad "psql PING via -rw service (phase=${phase_pod:-unknown})"
  fi
  $KUBECTL -n "$NS" delete pod pg-validate-cli --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
else
  bad "app secret $NAME-app not found"
fi

if [ "$FAILED" -eq 0 ]; then echo "=== pgsql-cnpg validate PASSED ==="; else echo "=== pgsql-cnpg validate FAILED ==="; fi
exit "$FAILED"
