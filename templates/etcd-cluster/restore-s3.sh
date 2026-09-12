#!/bin/bash
# etcd-cluster restore-s3 - S3 DR verification: download the latest snapshot
# (host s3cmd), then verify it inside a THROWAWAY helper pod (bitnami/etcd,
# pinned tag): etcdctl snapshot status (integrity), snapshot restore into a
# fresh data dir, single-node etcd boot + PUT/GET probe. The LIVE cluster is
# NOT touched. Exit code reflects the verification result.
set -euo pipefail

NAMESPACE="${ETCD_K8S_NAMESPACE:-{{ .Namespace }}}"
RELEASE="${ETCD_K8S_RELEASE:-{{ .Release }}}"
TAG="${ETCD_K8S_TAG:-{{ .Tag }}}"
ETCD_IMAGE="docker.io/bitnami/etcd:$TAG"
WORK_DIR="/tmp/etcd-restore"
LOCAL_SNAP="$WORK_DIR/snapshot.db"
HELPOD="${ETCD_RESTORE_POD:-etcd-restore-verify}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
SNAPSHOT="${1:-}"

[ -n "$S3_ENDPOINT" ]   || { echo "ERROR: S3_ENDPOINT must be set"; exit 1; }
[ -n "$S3_ACCESS_KEY" ] || { echo "ERROR: S3_ACCESS_KEY must be set"; exit 1; }
[ -n "$S3_SECRET_KEY" ] || { echo "ERROR: S3_SECRET_KEY must be set"; exit 1; }
command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not found on the node"; exit 1; }

export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
kctl() { k3s kubectl "$@"; }
HE() { kctl -n "$NAMESPACE" exec "$HELPOD" -- env ETCDCTL_API=3 /opt/bitnami/etcd/bin/etcdctl --command-timeout=8s "$@"; }
S3OPT=(--host="$S3_ENDPOINT" --host-bucket="%(bucket)s.$S3_ENDPOINT" --access_key="$S3_ACCESS_KEY" --secret_key="$S3_SECRET_KEY")

cleanup() {
  kctl -n "$NAMESPACE" delete pod "$HELPOD" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "=== etcd-cluster restore-s3 (DR verification) ==="

# 1. Locate the snapshot (arg = snapshot name, default: latest by S3 date).
if [ -n "$SNAPSHOT" ]; then
  SNAP_KEY="s3://$S3_BUCKET/$S3_PREFIX/$SNAPSHOT"
else
  echo "--- Step 1: latest snapshot in s3://$S3_BUCKET/$S3_PREFIX/ ---"
  S3_LISTING=$(s3cmd "${S3OPT[@]}" ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null || true)
  SNAP_KEY=$(echo "$S3_LISTING" | grep '\.db' | sort -k1,1r -k2,2r | head -1 | awk '{print $NF}')
  [ -n "$SNAP_KEY" ] || { echo "ERROR: no snapshots in s3://$S3_BUCKET/$S3_PREFIX/"; exit 1; }
fi
echo "  using: $SNAP_KEY"

# 2. Download.
echo "--- Step 2: Download ---"
mkdir -p "$WORK_DIR"
s3cmd "${S3OPT[@]}" get "$SNAP_KEY" "$LOCAL_SNAP" --force
echo "  [OK] downloaded: $LOCAL_SNAP ($(du -h "$LOCAL_SNAP" | cut -f1))"

# 3. Throwaway helper pod (pinned bitnami/etcd image).
echo "--- Step 3: helper pod ($HELPOD) ---"
kctl -n "$NAMESPACE" delete pod "$HELPOD" --ignore-not-found --wait=false >/dev/null 2>&1 || true
kctl -n "$NAMESPACE" run "$HELPOD" --image="$ETCD_IMAGE" --restart=Never --command -- sleep 1800
kctl -n "$NAMESPACE" wait --for=condition=Ready "pod/$HELPOD" --timeout=180s
kctl -n "$NAMESPACE" cp "$LOCAL_SNAP" "$HELPOD:/tmp/snapshot.db"

# 4. Integrity: etcdctl snapshot status must produce readable counters.
echo "--- Step 4: snapshot status (integrity) ---"
if ! HE snapshot status /tmp/snapshot.db 2>/dev/null | grep -q "totalKey"; then
  echo "  [X] snapshot status failed - corrupt or incomplete snapshot"
  exit 1
fi
echo "  [OK] snapshot integrity verified (hash/revision/counters readable)"

# 5. Restore into a fresh data dir + boot a single-node etcd for verification.
echo "--- Step 5: snapshot restore + single-node verification ---"
HE snapshot restore /tmp/snapshot.db \
  --name etcd-verify \
  --initial-cluster etcd-verify=http://127.0.0.1:2380 \
  --initial-advertise-peer-urls http://127.0.0.1:2380 \
  --data-dir /tmp/verify-data >/dev/null
kctl -n "$NAMESPACE" exec "$HELPOD" -- sh -c 'nohup /opt/bitnami/etcd/bin/etcd \
  --name etcd-verify --data-dir /tmp/verify-data \
  --listen-client-urls http://127.0.0.1:2379 --advertise-client-urls http://127.0.0.1:2379 \
  --listen-peer-urls http://127.0.0.1:2380 --initial-advertise-peer-urls http://127.0.0.1:2380 \
  --initial-cluster etcd-verify=http://127.0.0.1:2380 --initial-cluster-state new \
  >/tmp/etcd.log 2>&1 &'

VERIFIED=0
i=0
while [ "$i" -lt 30 ]; do
  if HE --endpoints=127.0.0.1:2379 endpoint health 2>/dev/null | grep -q ' is healthy'; then
    VERIFIED=1
    break
  fi
  i=$((i + 1))
  sleep 2
done
if [ "$VERIFIED" -ne 1 ]; then
  echo "  [X] single-node etcd did not come up after restore"
  echo "      (pod log: kubectl -n $NAMESPACE logs $HELPOD)"
  exit 1
fi
echo "  [OK] restored single-node etcd healthy"

# 6. PUT/GET probe against the restored data.
VERIFY_VAL="verify-$(date +%s)"
VERIFY_KEY="/_sdkops-restore-verify/$VERIFY_VAL"
if HE --endpoints=127.0.0.1:2379 put "$VERIFY_KEY" "$VERIFY_VAL" >/dev/null 2>&1 &&
   [ "$(HE --endpoints=127.0.0.1:2379 get "$VERIFY_KEY" --print-value-only 2>/dev/null)" = "$VERIFY_VAL" ]; then
  echo "  [OK] put/get on the restored data works ($VERIFY_VAL)"
else
  echo "  [X] put/get on the restored data failed"
  exit 1
fi

echo
echo "=== restore-s3 complete ==="
echo "  Verified snapshot: $SNAP_KEY"
echo "  The live cluster was not modified. To rebuild the live members from this"
echo "  snapshot: scale the StatefulSet down, restore each data dir, scale back up"
echo "  (manual step - see README gaps)."
