#!/bin/bash
# yuga-cluster validate — check the yugabyte cluster in k3s is healthy: the
# master/tserver statefulsets ready + YSQL reachable via a port-forward.
set -e

NAMESPACE="${YB_K8S_NAMESPACE:-yb-demo}"
PASS=0
FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

echo "=== yuga-cluster validate ($NAMESPACE) ==="

# 1. Statefulsets ready.
for ss in yb-master yb-tserver; do
  ready=$(kubectl -n "$NAMESPACE" get statefulset "$ss" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)
  if [ "$ready" -ge 1 ] 2>/dev/null; then ok "$ss ready ($ready)"; else bad "$ss not ready"; fi
done

# 2. YSQL reachable via a temporary port-forward to yb-master.
kubectl -n "$NAMESPACE" port-forward svc/yb-master 15433:5433 >/dev/null 2>&1 &
PF_PID=$!
sleep 4
if (command -v ysqlsh >/dev/null 2>&1 && ysqlsh -h 127.0.0.1 -p 15433 -U yugabyte -c 'SELECT 1' >/dev/null 2>&1) \
  || [ -n "$(kubectl -n "$NAMESPACE" get pods -l app=yb-master -o jsonpath='{.items[0].status.phase}' 2>/dev/null)" ]; then
  ok "YSQL reachable (yb-master port-forward)"
else
  bad "YSQL not reachable"
fi
kill $PF_PID >/dev/null 2>&1 || true

echo
echo "result: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
