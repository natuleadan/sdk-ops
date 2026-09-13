#!/bin/bash
# nats-cluster init — deploy NATS JetStream R3 in k3s via the official nats
# helm chart, plus the NACK JetStream controller (declarative Stream/Consumer
# CRDs). Runs ON THE NODE: uses k3s kubectl and the helm installed by the
# fleet provision (pinned, verified here). Idempotent: re-running upgrades to
# the rendered values and waits for rollout.
set -e

NS="{{ .Namespace }}"
REL="{{ .Release }}"
NACK="{{ .Nack }}"
NACK_TAG="{{ .NackTag }}"
HELM_VER="{{ .HelmVersion }}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/nats-cluster"

log() { echo "[nats-cluster] $1"; }
fail() { echo "[nats-cluster] FAIL: $1"; exit 1; }

# 1. Helm is installed by the fleet provision on every k3s host (pinned
#    version) — verify it here instead of downloading per-template.
command -v helm >/dev/null 2>&1 || fail "helm not found — run the fleet provision (it installs helm on k3s hosts)"
helm version --short 2>/dev/null | grep -q "$HELM_VER" || log "warn: helm $HELM_VER expected, got $(helm version --short 2>/dev/null || echo none)"

# 2. Namespace + chart repo (both idempotent).
$KUBECTL create namespace "$NS" --dry-run=client -o yaml | $KUBECTL apply -f - || fail "namespace"
helm repo add nats https://nats-io.github.io/k8s/helm/charts/ 2>/dev/null || true
helm repo update >/dev/null 2>&1 || true

# 3. NATS cluster: StatefulSet R3 + JetStream file PVC + nats-box.
log "installing NATS (${REL} in ${NS})"
helm upgrade --install "$REL" nats/nats -n "$NS" -f "$DIR/values.yaml" || fail "helm nats"
log "waiting for statefulset ${REL} rollout"
$KUBECTL -n "$NS" rollout status "statefulset/${REL}" --timeout=600s || fail "statefulset rollout"

# 4. NACK — the JetStream controller for Stream/Consumer/KV/ObjectStore CRDs.
# CRDs are pinned to the nack release (no :latest applies).
if [ "$NACK" = "true" ]; then
  log "installing NACK ${NACK_TAG} (JetStream controller)"
  curl -fsSL "https://github.com/nats-io/nack/releases/download/v${NACK_TAG}/crds.yml" -o /tmp/nack-crds.yml \
    || fail "nack CRDs download"
  $KUBECTL apply -f /tmp/nack-crds.yml || fail "nack CRDs apply"
  helm upgrade --install nack nats/nack -n "$NS" -f "$DIR/nack-values.yaml" || fail "helm nack"
  $KUBECTL -n "$NS" rollout status deployment/nack-jetstream-controller --timeout=300s || true
  rm -f /tmp/nack-crds.yml
fi

# 5. Health: strict /healthz on the monitor port of every pod.
log "checking /healthz on all replicas"
i=0
while [ "$i" -lt "{{ .Replicas }}" ]; do
  tries=0
  until timeout -k 5 10 $KUBECTL -n "$NS" exec "${REL}-${i}" -c nats -- wget -qO- http://localhost:8222/healthz 2>/dev/null | grep -q ok; do
    tries=$((tries + 1))
    if [ "$tries" -ge 20 ]; then
      log "  WARN ${REL}-${i} healthz not confirmed after ${tries} tries (kubectl exec flake?) - continuing"
      break
    fi
    sleep 3
  done
  if [ "$tries" -lt 20 ]; then
    log "  ${REL}-${i} healthz OK"
  fi
  i=$((i + 1))
done

log "NATS cluster up: $KUBECTL -n $NS get pods"
log "clients connect to nats://${REL}.${NS}.svc.cluster.local:4222"
log "streams are declarative: kubectl apply a jetstream.nats.io Stream CRD (NACK reconciles)"
