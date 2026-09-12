#!/bin/bash
# etcd-cluster backup-s3 - etcdctl snapshot save inside member-0, copy the
# file out of the cluster (kubectl cp) and upload it to S3 with the host
# s3cmd. Simple local retention (old .db files removed). Secrets come from
# the environment (S3_*) - never stored in these files.
set -euo pipefail

NAMESPACE="${ETCD_K8S_NAMESPACE:-{{ .Namespace }}}"
RELEASE="${ETCD_K8S_RELEASE:-{{ .Release }}}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
RETENTION="${RETENTION:-7}"

[ -n "$S3_ENDPOINT" ]   || { echo "ERROR: S3_ENDPOINT must be set"; exit 1; }
[ -n "$S3_ACCESS_KEY" ] || { echo "ERROR: S3_ACCESS_KEY must be set"; exit 1; }
[ -n "$S3_SECRET_KEY" ] || { echo "ERROR: S3_SECRET_KEY must be set"; exit 1; }
command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not found on the node"; exit 1; }

export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
kctl() { k3s kubectl "$@"; }
if [[ "$RELEASE" == *etcd* ]]; then STS="$RELEASE"; else STS="${RELEASE}-etcd"; fi
S3OPT=(--host="$S3_ENDPOINT" --host-bucket="%(bucket)s.$S3_ENDPOINT" --access_key="$S3_ACCESS_KEY" --secret_key="$S3_SECRET_KEY")

echo "=== etcd-cluster backup-s3 ==="

# 1. Snapshot via etcdctl (goes through the quorum) + copy out of the cluster.
echo "--- Step 1: snapshot save (pod ${STS}-0) ---"
mkdir -p "$BACKUP_DIR"
SNAP_NAME="etcd-$(hostname | tr -d '.')-$(date +%F-%H%M%S).db"
kctl -n "$NAMESPACE" exec "${STS}-0" -- env ETCDCTL_API=3 /opt/bitnami/etcd/bin/etcdctl --command-timeout=120s snapshot save /tmp/etcd-backup.db
kctl -n "$NAMESPACE" cp "${STS}-0:/tmp/etcd-backup.db" "$BACKUP_DIR/$SNAP_NAME"
kctl -n "$NAMESPACE" exec "${STS}-0" -- rm -f /tmp/etcd-backup.db
echo "  Local: $BACKUP_DIR/$SNAP_NAME ($(du -h "$BACKUP_DIR/$SNAP_NAME" | cut -f1))"

# 2. Upload to S3 + verify it appears in the listing.
echo "--- Step 2: Upload to S3 ---"
s3cmd "${S3OPT[@]}" put "$BACKUP_DIR/$SNAP_NAME" "s3://$S3_BUCKET/$S3_PREFIX/$SNAP_NAME" --no-progress
S3_LISTING=$(s3cmd "${S3OPT[@]}" ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null || true)
if echo "$S3_LISTING" | grep -q "$SNAP_NAME"; then
  echo "  [OK] uploaded + verified: s3://$S3_BUCKET/$S3_PREFIX/$SNAP_NAME"
else
  echo "  [X] upload verification failed"
  exit 1
fi

# 3. Local retention (keep the newest RETENTION snapshots on disk).
echo "--- Step 3: Local retention (keep last $RETENTION) ---"
OLD_SNAPS=$(ls -t "$BACKUP_DIR"/etcd-*.db 2>/dev/null | tail -n +$((RETENTION + 1)) || true)
for f in $OLD_SNAPS; do
  echo "  removing old: $f"
  rm -f "$f"
done

echo
echo "=== backup-s3 complete ==="
echo "  S3: s3://$S3_BUCKET/$S3_PREFIX/$SNAP_NAME"
echo "  Restore: bash restore-s3.sh [snapshot]"
