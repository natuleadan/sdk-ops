#!/bin/bash
# etcd-cluster init - deploy etcd inside k3s via the official bitnami helm
# chart: an external DCS for services running in the cluster that need etcd
# (k3s itself keeps its own embedded one - this does not touch it).
# Runs ON the k3s server node: k3s kubectl + the helm installed by the fleet
# provision (pinned, verified here).
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

# 1. Helm is installed by the fleet provision on every k3s host (pinned
#    version) — verify it here instead of downloading per-template.
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm not found — run the fleet provision (it installs helm on k3s hosts)"; exit 1; }
helm version --short 2>/dev/null | grep -q "$HELM_VERSION" || echo "WARN: helm $HELM_VERSION expected, got $(helm version --short 2>/dev/null || echo none)"

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
