#!/bin/sh
# etcd-bare backup-s3 - snapshot + upload to S3 + retention. Uses s3cmd on the
# host (~/.s3cfg, like the other bare templates). External S3 only - no
# embedded object storage. Credentials from S3_* env, never in files. Run as root.
set -e

BACKUP_DIR="${BACKUP_DIR:-/opt/backups/etcd}"
S3_BUCKET="${S3_BUCKET:-etcd-backups}"
S3_PREFIX="${S3_PREFIX:-etcd}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_CFG="${S3_CFG:-$HOME/.s3cfg}"
RETENTION="${RETENTION:-7}"

command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not installed (install it or run init.sh)"; exit 1; }

ensure_s3cfg() {
  # An existing ~/.s3cfg (operator-managed) always wins.
  if [ -s "$S3_CFG" ]; then
    return 0
  fi
  if [ -z "$S3_ENDPOINT" ] || [ -z "$S3_ACCESS_KEY" ] || [ -z "$S3_SECRET_KEY" ]; then
    echo "ERROR: $S3_CFG missing and S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY not set"
    return 1
  fi
  umask 077
  cat > "$S3_CFG" <<EOF
[default]
access_key = $S3_ACCESS_KEY
secret_key = $S3_SECRET_KEY
host_base = $S3_ENDPOINT
host_bucket = %(bucket)s.$S3_ENDPOINT
use_https = True
EOF
  echo "  -> wrote $S3_CFG from S3_* env"
}

S3="s3cmd -c $S3_CFG"

echo "=== etcd-bare backup-s3 ==="

echo "--- Step 1: Snapshot ---"
bash "$(dirname "$0")/snapshot.sh" || exit 1
SNAP_FILE=$(ls -t "$BACKUP_DIR"/etcd-*.db 2>/dev/null | head -1)
[ -n "$SNAP_FILE" ] || { echo "ERROR: no snapshot found after snapshot.sh"; exit 1; }
UPLOAD_NAME=$(basename "$SNAP_FILE")

echo "--- Step 2: Upload to S3 ---"
ensure_s3cfg
if ! $S3 info "s3://$S3_BUCKET" >/dev/null 2>&1; then
  $S3 mb "s3://$S3_BUCKET" >/dev/null 2>&1 || { echo "ERROR: cannot access/create bucket $S3_BUCKET"; exit 1; }
fi
if ! $S3 --no-progress put "$SNAP_FILE" "s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME" >/dev/null 2>&1; then
  echo "ERROR: S3 upload failed"
  exit 1
fi
echo "  Uploaded: s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME"

echo "--- Step 3: Retention (keep last $RETENTION) ---"
$S3 ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | awk '{print $NF}' | grep '\.db$' | sort -r | tail -n +$((RETENTION + 1)) | while read -r key; do
  [ -n "$key" ] || continue
  echo "  Removing old: $key"
  $S3 --no-progress del "$key" >/dev/null 2>&1 || true
done

rm -f "$SNAP_FILE"

echo ""
echo "=== backup-s3 complete ==="
echo "  S3: s3://$S3_BUCKET/$S3_PREFIX/$UPLOAD_NAME"
echo "  Restore: bash restore-s3.sh"
