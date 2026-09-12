#!/bin/bash
# etcd snapshot — local snapshot of the DCS data (etcdctl snapshot save).
set -u

BACKUP_DIR="${BACKUP_DIR:-./backups}"
SNAP_NAME="etcd-$(hostname | tr -d '.')-$(date +%F-%H%M%S).db"
LOCAL_CONTAINER=$(docker ps --format '{{"{{"}}.Names{{"}}"}}' 2>/dev/null | grep -E 'etcd' | head -1)

[ -n "$LOCAL_CONTAINER" ] || { echo "FAIL: no local etcd container"; exit 1; }
mkdir -p "$BACKUP_DIR"

echo "=== etcd snapshot ==="
docker exec "$LOCAL_CONTAINER" etcdctl --endpoints=127.0.0.1:2379 snapshot save "/tmp/$SNAP_NAME" >/dev/null 2>&1 || {
  echo "FAIL: snapshot save"
  exit 1
}
docker cp "$LOCAL_CONTAINER:/tmp/$SNAP_NAME" "$BACKUP_DIR/$SNAP_NAME" >/dev/null 2>&1 || {
  echo "FAIL: snapshot copy out of container"
  exit 1
}
docker exec "$LOCAL_CONTAINER" rm -f "/tmp/$SNAP_NAME" >/dev/null 2>&1 || true

echo "  [OK] $BACKUP_DIR/$SNAP_NAME ($(du -h "$BACKUP_DIR/$SNAP_NAME" | cut -f1))"
echo "  Upload: bash backup-s3.sh"
