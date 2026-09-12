#!/bin/bash
# etcd-cluster validate - cluster health from the node (k3s kubectl):
# every member pod Ready + etcdctl endpoint health across the whole
# endpoint list (quorum). Exit code 0 only when everything passes.
set -u

NAMESPACE="${ETCD_K8S_NAMESPACE:-{{ .Namespace }}}"
RELEASE="${ETCD_K8S_RELEASE:-{{ .Release }}}"
REPLICAS="${ETCD_K8S_REPLICAS:-{{ .Replicas }}}"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
kctl() { k3s kubectl "$@"; }
ECTL() { kctl -n "$NAMESPACE" exec "$1" -- env ETCDCTL_API=3 /opt/bitnami/etcd/bin/etcdctl --command-timeout=8s "${@:2}"; }

if [[ "$RELEASE" == *etcd* ]]; then STS="$RELEASE"; else STS="${RELEASE}-etcd"; fi
HEADLESS="${STS}-headless"

PASS=0
FAIL=0
ok()  { echo "  [OK] $1"; PASS=$((PASS+1)); }
bad() { echo "  [X] $1";  FAIL=$((FAIL+1)); }

# Dynamic member endpoint list (headless service DNS).
ENDPOINTS=""
i=0
while [ "$i" -lt "$REPLICAS" ]; do
  EP="${STS}-${i}.${HEADLESS}.${NAMESPACE}.svc.cluster.local:2379"
  if [ -z "$ENDPOINTS" ]; then ENDPOINTS="$EP"; else ENDPOINTS="$ENDPOINTS,$EP"; fi
  i=$((i + 1))
done

echo "=== etcd-cluster validate (namespace: $NAMESPACE) ==="
echo "  endpoints: $ENDPOINTS"

# 1. Every member pod Ready.
i=0
while [ "$i" -lt "$REPLICAS" ]; do
  READY=$(kctl -n "$NAMESPACE" get pod "${STS}-${i}" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null || echo "")
  if [ "$READY" = "true" ]; then ok "pod ${STS}-${i} Ready"; else bad "pod ${STS}-${i} not Ready"; fi
  i=$((i + 1))
done

# 2. Cluster health from member-0 across all endpoints.
HEALTHY=$(ECTL "${STS}-0" --endpoints="$ENDPOINTS" endpoint health 2>/dev/null | grep -c ' is healthy' || true)
if [ "$HEALTHY" = "$REPLICAS" ]; then
  ok "endpoint health: $HEALTHY/$REPLICAS healthy (quorum intact)"
else
  bad "endpoint health: $HEALTHY/$REPLICAS healthy"
fi

echo
echo "result: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
