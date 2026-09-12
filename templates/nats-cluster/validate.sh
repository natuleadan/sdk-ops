#!/bin/bash
# nats-cluster validate — k3s statefulset + JetStream + NACK controller checks.
# Runs on the node. Exit code real: 0 = healthy, 1 = failed.
set -u

NS="${NATS_K8S_NAMESPACE:-nats}"
REL="${NATS_K8S_RELEASE:-nats}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
NACK="${NATS_K8S_NACK:-true}"

FAILED=0
ok()   { echo "  [OK] $1"; }
bad()  { echo "  [FAIL] $1"; FAILED=1; }
checkv() { desc="$2"; if "$1" >/dev/null 2>&1; then ok "$desc"; else bad "$desc"; fi; }

echo "=== nats-cluster validate (ns=$NS rel=$REL) ==="

# 1. StatefulSet ready: R/R pods.
want="${NATS_K8S_REPLICAS:-3}"
ready="$($KUBECTL -n "$NS" get statefulset "$REL" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)"
if [ "$ready" = "$want" ]; then ok "statefulset $REL ready $ready/$want"; else bad "statefulset $REL ready ${ready:-0}/$want"; fi

# 2. Strict /healthz on every pod (checks meta + every stream/consumer asset).
i=0
while [ "$i" -lt "$want" ]; do
  h="$($KUBECTL -n "$NS" exec "${REL}-${i}" -c nats -- wget -qO- http://localhost:8222/healthz 2>/dev/null || true)"
  case "$h" in *ok*) ok "${REL}-${i} /healthz ok" ;; *) bad "${REL}-${i} /healthz: ${h:-no response}" ;; esac
  i=$((i + 1))
done

# 3. Client roundtrip through the ClusterIP service via nats-box.
if $KUBECTL -n "$NS" get deploy "${REL}-box" >/dev/null 2>&1; then
  if $KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats --server="nats://${REL}.${NS}.svc:4222" pub healthcheck.ping hello >/dev/null 2>&1; then
    ok "pub via ${REL}-box -> ${REL}.${NS}.svc:4222"
  else
    bad "pub via nats-box"
  fi
  # 4. JetStream enabled + serving.
  js="$($KUBECTL -n "$NS" exec "deploy/${REL}-box" -- nats --server="nats://${REL}.${NS}.svc:4222" account info 2>/dev/null | grep -c "JetStream" || true)"
  if [ "${js:-0}" -ge 1 ]; then ok "JetStream account active"; else bad "JetStream account"; fi
else
  bad "nats-box deployment missing (natsBox disabled?)"
fi

# 5. NACK controller + CRDs.
if [ "$NACK" = "true" ]; then
  if $KUBECTL -n "$NS" rollout status deployment/nack --timeout=10s >/dev/null 2>&1; then
    ok "NACK controller ready"
  else
    bad "NACK controller"
  fi
  if $KUBECTL get crd streams.jetstream.nats.io >/dev/null 2>&1; then
    ok "Stream CRD registered"
  else
    bad "Stream CRD (jetstream.nats.io)"
  fi
fi

if [ "$FAILED" -eq 0 ]; then echo "=== nats-cluster validate PASSED ==="; else echo "=== nats-cluster validate FAILED ==="; fi
exit "$FAILED"
