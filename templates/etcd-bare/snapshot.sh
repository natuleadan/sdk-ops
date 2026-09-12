#!/bin/bash
# etcd-bare snapshot - local snapshot of the DCS data (etcdctl snapshot save,
# API v3; v2 stays enabled for Patroni but snapshots always use v3). Run as root.
set -u

BACKUP_DIR="${BACKUP_DIR:-/opt/backups/etcd}"
CTL="${ETCDCTL:-/usr/local/bin/etcdctl}"
export ETCDCTL_API=3
SNAP_NAME="etcd-$(hostname | tr -d '.')-$(date +%F-%H%M%S).db"

systemctl is-active --quiet etcd || { echo "FAIL: etcd unit not running"; exit 1; }
mkdir -p "$BACKUP_DIR"

echo "=== etcd-bare snapshot ==="
"$CTL" --endpoints=127.0.0.1:2379 --command-timeout=120s snapshot save "$BACKUP_DIR/$SNAP_NAME" >/dev/null 2>&1 || {
  echo "FAIL: snapshot save"
  rm -f "$BACKUP_DIR/$SNAP_NAME"
  exit 1
}
if ! "$CTL" --endpoints=127.0.0.1:2379 snapshot status "$BACKUP_DIR/$SNAP_NAME" 2>/dev/null | grep -q totalKey; then
  echo "FAIL: snapshot integrity (status unreadable)"
  rm -f "$BACKUP_DIR/$SNAP_NAME"
  exit 1
fi

echo "  [OK] $BACKUP_DIR/$SNAP_NAME ($(du -h "$BACKUP_DIR/$SNAP_NAME" | cut -f1))"
echo "  Upload: bash backup-s3.sh"
