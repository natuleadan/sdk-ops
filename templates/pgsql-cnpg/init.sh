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

{{- if .BackupEnabled }}
# 3b. A FRESH bootstrap needs an EMPTY barman store: barman-cloud refuses to
#     archive over an existing one ("Expected empty archive") and CNPG retries
#     in a loop that hammers the kube-apiserver (observed: io wait 100% and
#     every kubectl call timing out). Fail fast with a clear message instead;
#     an existing cluster is a normal re-provision and skips this check.
if ! $KUBECTL -n "$NS" get cluster "$NAME" >/dev/null 2>&1; then
  if command -v s3cmd >/dev/null 2>&1 && [ -f "$HOME/.s3cfg" ]; then
    FIRST="$(s3cmd ls "s3://{{ .S3Bucket }}/{{ .S3Prefix }}/" 2>/dev/null | head -1)"
    if [ -n "$FIRST" ]; then
      fail "S3 prefix s3://{{ .S3Bucket }}/{{ .S3Prefix }}/ is not empty - a fresh cluster needs an empty barman store. Clean it (s3cmd del --recursive --force s3://{{ .S3Bucket }}/{{ .S3Prefix }}/) or deploy with a fresh PG_S3_PREFIX."
    fi
  fi
fi
{{- end }}

# 4. The Cluster CR (instances, storage, resources, backups). The admission
#    webhook may still be warming up right after the operator rollout on slow
#    nodes (first image pull): retry instead of failing the provision.
cr_ok=0
for i in 1 2 3 4 5 6; do
  if $KUBECTL apply -f "$DIR/cluster.yaml"; then cr_ok=1; break; fi
  log "Cluster CR apply retry $i (webhook warming up)"
  sleep 10
done
[ "$cr_ok" = 1 ] || fail "Cluster CR apply"

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
