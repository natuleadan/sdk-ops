#!/bin/bash
# pgsql-cnpg init — CloudNativePG operator (pinned release manifest) + the
# Cluster CR + the S3 credentials secret. Runs ON THE NODE with k3s kubectl.
# Idempotent: the operator is a server-side apply, the CR is upgraded by
# re-applying.
set -e

NS="{{ .Namespace }}"
NAME="{{ .Name }}"
OPERATOR_MANIFEST="{{ .OperatorManifest }}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/pgsql-cnpg"

log() { echo "[pgsql-cnpg] $1"; }
fail() { echo "[pgsql-cnpg] FAIL: $1"; exit 1; }

# 0. Secrets from the wiring env (S3_* — never in the YAML).
[ -f "$DIR/.env" ] && . "$DIR/.env"

# 1. Namespace.
$KUBECTL create namespace "$NS" --dry-run=client -o yaml | $KUBECTL apply -f - || fail "namespace"

# 2. The CloudNativePG operator (CRDs + controller, pinned release). The CRDs
#    exceed the client-side apply annotation limit — server-side apply.
if ! $KUBECTL get crd clusters.postgresql.cnpg.io >/dev/null 2>&1; then
  log "installing CloudNativePG operator ($OPERATOR_MANIFEST)"
  curl -fsSL "$OPERATOR_MANIFEST" -o /tmp/cnpg-operator.yaml || fail "operator manifest download"
  $KUBECTL apply --server-side -f /tmp/cnpg-operator.yaml || fail "operator apply"
  rm -f /tmp/cnpg-operator.yaml
  log "waiting for the operator to come up"
  $KUBECTL -n cnpg-system rollout status deployment/cnpg-controller-manager --timeout=300s || fail "operator rollout"
else
  log "operator already installed"
fi

# 3. S3 credentials secret (only when backups are enabled in the CR).
{{- if .BackupEnabled }}
if [ -n "${S3_ACCESS_KEY:-}" ] && [ -n "${S3_SECRET_KEY:-}" ]; then
  $KUBECTL -n "$NS" create secret generic "$NAME-s3" \
    --from-literal=ACCESS_KEY_ID="$S3_ACCESS_KEY" \
    --from-literal=ACCESS_SECRET_KEY="$S3_SECRET_KEY" \
    --dry-run=client -o yaml | $KUBECTL apply -f - || fail "s3 secret"
else
  log "WARN: S3_ACCESS_KEY/S3_SECRET_KEY not set — backups will fail until present"
fi
{{- end }}

# 4. The Cluster CR (instances, storage, resources, backups).
$KUBECTL apply -f "$DIR/cluster.yaml" || fail "Cluster CR apply"

# 5. Wait for every instance to be ready.
log "waiting for $NAME ({{ .Instances }} instances)"
deadline=$((SECONDS + 600))
ready=0
while [ "$SECONDS" -lt "$deadline" ]; do
  ready="$($KUBECTL -n "$NS" get cluster "$NAME" -o jsonpath='{.status.readyInstances}' 2>/dev/null || echo 0)"
  [ "${ready:-0}" -ge "{{ .Instances }}" ] && break
  sleep 5
done
[ "${ready:-0}" -ge "{{ .Instances }}" ] || fail "cluster not ready ($ready/{{ .Instances }})"

log "Cluster up: clients connect to $NAME-rw.$NS.svc:5432 (app secret: $NAME-app)"
