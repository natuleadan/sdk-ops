#!/bin/bash
# nats-cluster init — deploy NATS JetStream R3 in k3s via the official nats
# helm chart, plus the NACK JetStream controller (declarative Stream/Consumer
# CRDs). Runs ON THE NODE: uses k3s kubectl and a locally installed helm
# (auto-downloaded, pinned). Idempotent: re-running upgrades to the rendered
# values and waits for rollout.
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

# 1. Ensure helm (pinned version, amd64/arm64) — the k3s node may not have it.
if ! command -v /usr/local/bin/helm >/dev/null 2>&1; then
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64)  HARCH="amd64" ;;
    aarch64|arm64) HARCH="arm64" ;;
    *) fail "unsupported arch $ARCH" ;;
  esac
  log "installing helm ${HELM_VER} (${HARCH})"
  curl -fsSL "https://get.helm.sh/helm-${HELM_VER}-linux-${HARCH}.tar.gz" -o /tmp/helm.tgz || fail "helm download"
  tar -xzf /tmp/helm.tgz -C /tmp
  mv "/tmp/linux-${HARCH}/helm" /usr/local/bin/helm
  rm -rf /tmp/helm.tgz "/tmp/linux-${HARCH}"
fi

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
  until $KUBECTL -n "$NS" exec "${REL}-${i}" -c nats -- wget -qO- http://localhost:8222/healthz 2>/dev/null | grep -q ok; do
    sleep 3
  done
  log "  ${REL}-${i} healthz OK"
  i=$((i + 1))
done

log "NATS cluster up: $KUBECTL -n $NS get pods"
log "clients connect to nats://${REL}.${NS}.svc.cluster.local:4222"
log "streams are declarative: kubectl apply a jetstream.nats.io Stream CRD (NACK reconciles)"
