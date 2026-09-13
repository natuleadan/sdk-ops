#!/bin/bash
# df-cluster init — install the dragonflydb operator (pinned manifest) and
# apply the Dragonfly CR (rendered dragonfly.yaml). Runs ON THE NODE with
# k3s kubectl. Idempotent: the operator manifest is declarative, the CR is
# upgraded by re-applying.
set -e

NS="{{ .Namespace }}"
NAME="{{ .Name }}"
OPERATOR_MANIFEST="{{ .OperatorManifest }}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/df-cluster"

log() { echo "[df-cluster] $1"; }
fail() { echo "[df-cluster] FAIL: $1"; exit 1; }

# 0. Wiring env (DF_PASSWORD + S3_* — never in the YAML).
[ -f "$DIR/.env" ] && . "$DIR/.env"

# 0b. DR tooling: the explicit backup/restore scripts need s3cmd + its config
#     on the host (never manual setup). Only when S3 is wired.
if [ -n "${S3_BUCKET:-}" ] && [ -n "${S3_ENDPOINT:-}" ]; then
  if ! command -v s3cmd >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    sudo apt-get update -qq >/dev/null 2>&1 || true
    sudo apt-get install -y -qq s3cmd >/dev/null 2>&1 || log "WARN: s3cmd install failed - the DR scripts will need it"
  fi
  ES3="$(echo "$S3_ENDPOINT" | sed 's#https\?://##; s#/*$##')"
  cat > "$HOME/.s3cfg" <<EOF2
[default]
access_key = ${S3_ACCESS_KEY:-}
secret_key = ${S3_SECRET_KEY:-}
host_base = $ES3
host_bucket = %(bucket)s.$ES3
use_https = True
signature_v2 = False
EOF2
  chmod 600 "$HOME/.s3cfg"
  log "s3cmd DR tooling ready ($ES3)"
fi

# 1. Namespace.
$KUBECTL create namespace "$NS" --dry-run=client -o yaml | $KUBECTL apply -f - || fail "namespace"

# 2. The dragonflydb operator (CRDs + controller, pinned release).
if ! $KUBECTL get crd dragonflies.dragonflydb.io >/dev/null 2>&1; then
  log "installing dragonflydb operator ($OPERATOR_MANIFEST)"
  curl -fsSL "$OPERATOR_MANIFEST" -o /tmp/df-operator.yaml || fail "operator manifest download"
  # Patch kube-rbac-proxy image: gcr.io is often unreachable; use quay.io mirror.
  sed -i 's|gcr.io/kubebuilder/kube-rbac-proxy:v0.16.0|quay.io/brancz/kube-rbac-proxy:v0.16.0|g' /tmp/df-operator.yaml
  $KUBECTL apply -f /tmp/df-operator.yaml || fail "operator apply"
  rm -f /tmp/df-operator.yaml
  log "waiting for the operator to come up"
  $KUBECTL -n dragonfly-operator-system rollout status deployment/dragonfly-operator-controller-manager --timeout=300s || fail "operator rollout"
else
  log "operator already installed"
fi

# 3. The Dragonfly CR (primary + replicas, automatic failover, snapshots).
$KUBECTL apply -f "$DIR/dragonfly.yaml" || fail "Dragonfly CR apply"

# 4. Wait for the pods (df-0..N) to be READY (Running phase alone is not
# enough: a crash-looping pod is also "Running").
log "waiting for $NAME replicas"
deadline=$((SECONDS + 900))
ready=0
while [ "$SECONDS" -lt "$deadline" ]; do
  ready="$($KUBECTL -n "$NS" get pods -l "app.kubernetes.io/name=dragonfly" --no-headers 2>/dev/null | awk '$2 == "1/1"' | wc -l | tr -d ' ')"
  want="$($KUBECTL -n "$NS" get dragonfly "$NAME" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo 3)"
  [ "${ready:-0}" -ge "${want:-3}" ] && break
  sleep 10
done
[ "${ready:-0}" -ge "${want:-3}" ] || fail "pods not ready ($ready/${want:-3})"

log "Dragonfly up: k3s kubectl -n $NS get dragonfly"
log "clients connect to redis://$NAME.$NS.svc.cluster.local:6379"
