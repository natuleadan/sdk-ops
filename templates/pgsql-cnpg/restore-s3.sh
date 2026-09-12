#!/bin/bash
# pgsql-cnpg restore-s3 — DR recovery: bootstrap a NEW cluster from the S3
# barman object store (same destination the source cluster backs up to).
# Non-destructive: the original cluster keeps running; the recovery lands as
# <name>-restore. For a true same-name DR, delete the source cluster + its PVCs
# first, then run this with RESTORE_NAME=<name>.
#
# Usage: restore-s3.sh [--yes] [recovery-target-time RFC3339]
set -u

NS="${PG_K8S_NAMESPACE:-pg}"
NAME="${PG_K8S_NAME:-pg}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/pgsql-cnpg"
[ -f "$DIR/.env" ] && . "$DIR/.env"

S3_BUCKET="${S3_BUCKET:-pg-backups}"
S3_PREFIX="${S3_PREFIX:-pg}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
RCI="${RCI_IMAGE:-postgres:17-alpine}"
RESTORE_NAME="${RESTORE_NAME:-$NAME-restore}"
TARGET_TIME=""
YES=false

ensure_s3cfg() {
  [ -f "$HOME/.s3cfg" ] && return 0
  [ -n "${S3_ENDPOINT:-}" ] || return 0
  cat > "$HOME/.s3cfg" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $S3_ENDPOINT
host_bucket = %(bucket)s.$S3_ENDPOINT
use_https = True
EOF
  chmod 600 "$HOME/.s3cfg"
}

usage() {
  echo "Usage: restore-s3.sh [--yes] [recovery-target-time RFC3339]"
  echo ""
  echo "Bootstraps $RESTORE_NAME from s3://\$S3_BUCKET/\$S3_PREFIX (barman store)."
  echo "A recovery target time enables point-in-time recovery (default: latest)."
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=true; shift ;;
    --help|-h) usage ;;
    *) TARGET_TIME="$1"; shift ;;
  esac
done

echo "=== pgsql-cnpg restore-s3 ==="

[ -n "$S3_ENDPOINT" ] || { echo "ERROR: S3_ENDPOINT not set (source .env or export)"; exit 1; }
$KUBECTL -n "$NS" get secret "$NAME-s3" >/dev/null 2>&1 || { echo "ERROR: secret $NAME-s3 missing"; exit 1; }

echo "--- Step 1: verify the S3 store ---"
if command -v s3cmd >/dev/null 2>&1; then
  ensure_s3cfg
  LATEST="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/$NAME/base/" 2>/dev/null | tail -1 | awk '{print $2}')"
  [ -n "$LATEST" ] || { echo "ERROR: no base backups in s3://$S3_BUCKET/$S3_PREFIX/$NAME/base/"; exit 1; }
  echo "  latest base backup: $LATEST"
else
  echo "  [WARN] s3cmd not installed — skipping the store listing"
fi
[ -z "$TARGET_TIME" ] || echo "  recovery target time: $TARGET_TIME"

if [ "$YES" = false ]; then
  echo ""
  echo "This creates cluster $RESTORE_NAME in namespace $NS from the S3 backups."
  echo "Press Ctrl+C to cancel or Enter to continue."
  read -r _
fi

echo "--- Step 2: recovery Cluster CR ---"
TARGET_BLOCK=""
if [ -n "$TARGET_TIME" ]; then
  TARGET_BLOCK="      recoveryTarget:
        targetTime: \"$TARGET_TIME\"
"
fi
cat <<EOF | $KUBECTL apply -f - || { echo "ERROR: recovery CR apply failed"; exit 1; }
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: $RESTORE_NAME
  namespace: $NS
spec:
  instances: 1
  storage:
    size: 1Gi
    storageClass: local-path
  bootstrap:
    recovery:
      source: $NAME-s3
$TARGET_BLOCK  externalClusters:
    - name: $NAME-s3
      barmanObjectStore:
        destinationPath: "s3://$S3_BUCKET/$S3_PREFIX"
        # barman keys the store by serverName (defaults to the external cluster
        # name, here "$NAME-s3") — pin it to the source cluster name so the
        # recovery finds $S3_PREFIX/$NAME/{base,wals}.
        serverName: $NAME
        endpointURL: "https://$S3_ENDPOINT"
        s3Credentials:
          accessKeyId:
            name: $NAME-s3
            key: ACCESS_KEY_ID
          secretAccessKey:
            name: $NAME-s3
            key: ACCESS_SECRET_KEY
EOF

echo "--- Step 3: wait for the recovered cluster ---"
deadline=$((SECONDS + 900))
ready=0
while [ "$SECONDS" -lt "$deadline" ]; do
  ready="$($KUBECTL -n "$NS" get cluster "$RESTORE_NAME" -o jsonpath='{.status.readyInstances}' 2>/dev/null || echo 0)"
  [ "${ready:-0}" -ge 1 ] && break
  sleep 5
done
[ "${ready:-0}" -ge 1 ] || { echo "  [FAIL] restore not ready"; $KUBECTL -n "$NS" get cluster "$RESTORE_NAME" -o yaml 2>/dev/null | tail -20; exit 1; }

echo "  [OK] $RESTORE_NAME ready ($ready/1)"

echo "--- Step 4: sanity read --"
PASS="$(k3s kubectl -n "$NS" get secret "$RESTORE_NAME-app" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)"
if [ -n "$PASS" ]; then
  pod="pg-restore-check-$RANDOM"
  $KUBECTL -n "$NS" run "$pod" --restart=Never --image="$RCI" --command -- \
    psql "postgresql://app:$PASS@$RESTORE_NAME-rw.$NS.svc:5432/app" -tAc "SELECT current_database(), now()" >/dev/null 2>&1 || true
  phase=""
  for i in $(seq 1 15); do
    phase="$($KUBECTL -n "$NS" get pod "$pod" -o jsonpath='{.status.phase}' 2>/dev/null)"
    [ "$phase" = "Succeeded" ] && break
    [ "$phase" = "Failed" ] && break
    sleep 2
  done
  if [ "$phase" = "Succeeded" ]; then
    echo "  [OK] recovered cluster answers queries:"
    $KUBECTL -n "$NS" logs "$pod" 2>/dev/null | sed 's/^/    /'
  else
    echo "  [WARN] sanity query did not succeed (phase=${phase:-unknown})"
  fi
  $KUBECTL -n "$NS" delete pod "$pod" --force --grace-period=0 --ignore-not-found >/dev/null 2>&1
fi

echo ""
echo "=== restore-s3 complete ==="
echo "  recovered cluster: $RESTORE_NAME.$NS.svc (rw: $RESTORE_NAME-rw, ro: $RESTORE_NAME-ro)"
echo "  cleanup: $KUBECTL -n $NS delete cluster $RESTORE_NAME"
