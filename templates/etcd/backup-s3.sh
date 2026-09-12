#!/bin/sh
# etcd backup-s3 — snapshot + upload to S3 + retention.
# Uses s3cmd on the host (pre-installed, ~/.s3cfg). External S3 only.
set -u

BACKUP_DIR="${BACKUP_DIR:-./backups}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
RETENTION="${RETENTION:-7}"

echo "=== etcd backup-s3 ==="

command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not installed"; exit 1; }

bash "$(dirname "$0")/snapshot.sh" || exit 1

SNAP_FILE=$(ls -t "$BACKUP_DIR"/etcd-*.db 2>/dev/null | head -1)
[ -n "$SNAP_FILE" ] || { echo "ERROR: no snapshot found after snapshot.sh"; exit 1; }
UPLOAD_NAME=$(basename "$SNAP_FILE")

echo "--- Upload to S3 ---"
s3cmd put "$SNAP_FILE" "s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME" 2>&1
echo "  Uploaded: s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME"

echo "--- Retention (keep last $RETENTION) ---"
EXISTING=$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | grep '\.db' | awk '{print $4}' | sort -r)
COUNT=$(echo "$EXISTING" | grep -c . || true)
if [ "$COUNT" -gt "$RETENTION" ]; then
  echo "$EXISTING" | tail -n +$((RETENTION + 1)) | while read -r key; do
    [ -n "$key" ] && echo "  Removing old: $key" && s3cmd del "$key" 2>/dev/null || true
  done
fi

rm -f "$SNAP_FILE"

echo ""
echo "=== backup-s3 complete ==="
echo "  S3: s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME"
