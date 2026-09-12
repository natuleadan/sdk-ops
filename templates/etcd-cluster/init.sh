#!/bin/bash
# etcd-cluster init - deploy etcd inside k3s via the official bitnami helm
# chart: an external DCS for services running in the cluster that need etcd
# (k3s itself keeps its own embedded one - this does not touch it).
# Runs ON the k3s server node: k3s kubectl + helm (auto-installed here).
# Idempotent: re-runs converge to the same release (helm upgrade --install).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NAMESPACE="${ETCD_K8S_NAMESPACE:-{{ .Namespace }}}"
RELEASE="${ETCD_K8S_RELEASE:-{{ .Release }}}"
HELM_VERSION="${ETCD_K8S_HELM_VERSION:-{{ .HelmVersion }}}"

echo "=== etcd-cluster init ==="
echo "Namespace: $NAMESPACE  release: $RELEASE  helm: $HELM_VERSION"

command -v k3s >/dev/null 2>&1 || { echo "ERROR: k3s not found (this service runs on a k3s node)"; exit 1; }
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
kctl() { k3s kubectl "$@"; }

# Bitnami fullname logic: a release name containing "etcd" is used as-is,
# otherwise the release is suffixed with the chart name (<release>-etcd).
if [[ "$RELEASE" == *etcd* ]]; then STS="$RELEASE"; else STS="${RELEASE}-etcd"; fi

# 1. Ensure the helm binary ({{ .HelmVersion }}, linux amd64/arm64).
if ! command -v helm >/dev/null 2>&1; then
  echo "--- Installing helm $HELM_VERSION ---"
  case "$(uname -m)" in
    x86_64)        HELM_ARCH="amd64" ;;
    aarch64|arm64) HELM_ARCH="arm64" ;;
    *) echo "ERROR: unsupported arch: $(uname -m)"; exit 1 ;;
  esac
  HELM_TGZ="/tmp/helm-${HELM_VERSION}-linux-${HELM_ARCH}.tar.gz"
  HELM_URL="https://get.helm.sh/helm-${HELM_VERSION}-linux-${HELM_ARCH}.tar.gz"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$HELM_URL" -o "$HELM_TGZ"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$HELM_TGZ" "$HELM_URL"
  else
    echo "ERROR: curl or wget required to install helm"
    exit 1
  fi
  tar -xzf "$HELM_TGZ" -C /tmp
  install -m 0755 "/tmp/linux-${HELM_ARCH}/helm" /usr/local/bin/helm
  rm -rf "/tmp/linux-${HELM_ARCH}" "$HELM_TGZ"
fi

# 2. Namespace (dry-run apply keeps it idempotent).
kctl create namespace "$NAMESPACE" --dry-run=client -o yaml | kctl apply -f -

# 3. Install / upgrade the etcd release with the rendered values.
helm repo add bitnami https://charts.bitnami.com/bitnami >/dev/null 2>&1 || true
helm repo update bitnami >/dev/null
helm upgrade --install "$RELEASE" bitnami/etcd \
  --namespace "$NAMESPACE" \
  -f "$SCRIPT_DIR/values.yaml"

# 4. Wait for the member set (the statefulset rollout = quorum pods Ready).
echo "--- Waiting for the etcd members ---"
kctl -n "$NAMESPACE" rollout status "statefulset/$STS" --timeout=600s || \
  kctl -n "$NAMESPACE" wait --for=condition=ready pod -l "app.kubernetes.io/instance=$RELEASE" --timeout=600s

echo "  [OK] etcd deployed in k3s (namespace: $NAMESPACE, release: $RELEASE)"
echo "  Client svc: $STS.$NAMESPACE.svc.cluster.local:2379 (internal DNS, no host ports)"
echo "  Validate: bash validate.sh"
