#!/bin/bash
# yuga-bare deploy-k3s — deploy YugabyteDB inside an existing k3s cluster
# via the yugabyte-k8s-operator (helm). The operator manages a YBManaged CRD
# declaratively (the same role CloudNativePG plays for postgres). Run from the
# operator machine with kubectl pointing at the k3s cluster.
set -e

NAMESPACE="${YB_K8S_NAMESPACE:-yb-demo}"
RELEASE="${YB_K8S_RELEASE:-yb-demo}"
YB_TAG="${YB_TAG:-2026.1.1.1-b2}"
MASTER_REPLICAS="${YB_MASTER_REPLICAS:-1}"
TSERVER_REPLICAS="${YB_TSERVER_REPLICAS:-1}"

echo "=== yuga-bare deploy-k3s ==="
echo "Namespace: $NAMESPACE  release: $RELEASE  tag: $YB_TAG"

# 1. Add + update the helm chart repo (the yugabyte-k8s-operator).
helm repo add yugabytedb https://charts.yugabyte.com 2>/dev/null || true
helm repo update

# 2. Create the namespace + install the operator chart.
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install "$RELEASE" yugabytedb/yugabyte \
  --namespace "$NAMESPACE" \
  --set "Image.tag=$YB_TAG" \
  --set "replicas.master=$MASTER_REPLICAS" \
  --set "replicas.tserver=$TSERVER_REPLICAS" \
  --set "resource.master.requests.cpu=0.5,resource.master.requests.memory=0.5Gi" \
  --set "resource.tserver.requests.cpu=0.5,resource.tserver.requests.memory=0.5Gi"

# 3. Wait for the masters to come up.
echo "  → waiting for the yugabyte masters..."
kubectl -n "$NAMESPACE" rollout status statefulset/yb-master --timeout=300s || \
  kubectl -n "$NAMESPACE" wait --for=condition=ready pod -l app=yb-master -n "$NAMESPACE" --timeout=300s

echo "  yugabyte deployed in k3s ($NAMESPACE)"
echo "  YSQL: kubectl -n $NAMESPACE port-forward svc/yb-master 5433:5433"
echo "  UI:   kubectl -n $NAMESPACE port-forward svc/yb-master-ui 7000:7000"
