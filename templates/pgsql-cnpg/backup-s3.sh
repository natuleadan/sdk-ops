#!/bin/bash
# pgsql-cnpg backup-s3 — on-demand CNPG Backup (barman object store -> S3) and
# verification that the artifact landed in the bucket. The wal/data compression
# and retention policy come from the Cluster CR (backup.barmanObjectStore).
set -u

NS="${PG_K8S_NAMESPACE:-pg}"
NAME="${PG_K8S_NAME:-pg}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="/opt/sdk-ops/services/pgsql-cnpg"
[ -f "$DIR/.env" ] && . "$DIR/.env"

S3_BUCKET="${S3_BUCKET:-pg-backups}"
S3_PREFIX="${S3_PREFIX:-pg}"
TS="$(date +%F-%H%M%S)"
BACKUP_NAME="$NAME-manual-$TS"

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

echo "=== pgsql-cnpg backup-s3 ==="

$KUBECTL get crd backups.postgresql.cnpg.io >/dev/null 2>&1 || { echo "ERROR: CNPG not installed"; exit 1; }

echo "--- Step 1: on-demand Backup CR ($BACKUP_NAME) ---"
cat <<EOF | $KUBECTL apply -f - || { echo "ERROR: Backup CR apply failed"; exit 1; }
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: $BACKUP_NAME
  namespace: $NS
spec:
  cluster:
    name: $NAME
  method: barmanObjectStore
EOF

echo "--- Step 2: wait for completion ---"
deadline=$((SECONDS + 900))
phase=""
while [ "$SECONDS" -lt "$deadline" ]; do
  phase="$($KUBECTL -n "$NS" get backup "$BACKUP_NAME" -o jsonpath='{.status.phase}' 2>/dev/null)"
  [ "$phase" = "completed" ] && break
  [ "$phase" = "failed" ] && break
  sleep 5
done
if [ "$phase" = "completed" ]; then
  echo "  [OK] backup completed: $BACKUP_NAME"
else
  echo "  [FAIL] backup phase=${phase:-unknown}"
  $KUBECTL -n "$NS" describe backup "$BACKUP_NAME" 2>/dev/null | tail -10
  exit 1
fi

echo "--- Step 3: verify the artifact in S3 ---"
if command -v s3cmd >/dev/null 2>&1; then
  ensure_s3cfg
  LATEST="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/$NAME/base/" 2>/dev/null | tail -1 | awk '{print $2}')"
  if [ -n "$LATEST" ]; then
    echo "  [OK] S3 latest base backup: $LATEST"
  else
    echo "  [WARN] no base/ listing in s3://$S3_BUCKET/$S3_PREFIX/ yet (barman may lag)"
  fi
else
  echo "  [SKIP] s3cmd not installed on the host"
fi

echo ""
echo "=== backup-s3 complete ==="
echo "  backup CR: $NS/$BACKUP_NAME"
