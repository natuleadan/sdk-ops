#!/bin/sh
# df-bare backup-s3 - consistent snapshot (BGSAVE) + upload to S3 + retention.
# Uses s3cmd on the host (~/.s3cfg, like the other bare templates). External
# S3 only - no embedded object storage. Run as root.
set -e

DF_PASSWORD="${DF_PASSWORD:-}"
if [ -z "$DF_PASSWORD" ] && [ -r /etc/dragonfly/primary.conf ]; then
  DF_PASSWORD="$(sed -n 's/^--requirepass=//p' /etc/dragonfly/primary.conf | head -1)"
fi
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_DATA="${PRIMARY_DATA:-/var/lib/dragonfly/primary}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATE=$(date +%F-%H%M%S)
S3_BUCKET="${S3_BUCKET:-df-backups}"
S3_PREFIX="${S3_PREFIX:-df}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_CFG="${S3_CFG:-$HOME/.s3cfg}"
RETENTION="${RETENTION:-7}"

RC() { redis-cli -h 127.0.0.1 -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

command -v s3cmd >/dev/null 2>&1 || { echo "ERROR: s3cmd not installed (run init.sh)"; exit 1; }
command -v redis-cli >/dev/null 2>&1 || { echo "ERROR: redis-cli not installed (run init.sh)"; exit 1; }

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

echo "=== df-bare backup-s3 ==="

if ! systemctl is-active --quiet dragonfly-primary; then
  echo "ERROR: dragonfly-primary is not running"
  exit 1
fi

echo "--- Step 1: Consistent snapshot (BGSAVE) ---"
RC BGSAVE >/dev/null
echo -n "  Waiting for snapshot..."
i=0
while [ "$i" -lt 30 ]; do
  INPROGRESS=$(RC INFO PERSISTENCE | grep "rdb_bgsave_in_progress" | cut -d: -f2 | tr -d '\r\n')
  [ "$INPROGRESS" = "0" ] && break
  i=$((i + 1))
  sleep 1
done
echo " done"

# Dragonfly persists the snapshot set as dump-<ts>-*.dfs in the data dir.
TIMESTAMP=$(ls "$PRIMARY_DATA"/dump-*summary.dfs 2>/dev/null | sed "s/.*dump-//;s/-summary.*//" | sort | tail -1)
if [ -z "$TIMESTAMP" ]; then
  echo "ERROR: no snapshot files found in $PRIMARY_DATA (BGSAVE failed?)"
  exit 1
fi

mkdir -p "$BACKUP_DIR"
LOCAL_BACKUP="$BACKUP_DIR/df-$DATE.tar.gz"
(cd "$PRIMARY_DATA" && tar czf - dump-"$TIMESTAMP"-*.dfs) > "$LOCAL_BACKUP"
echo "  Local: $LOCAL_BACKUP ($(du -h "$LOCAL_BACKUP" | cut -f1))"

echo "--- Step 2: Upload to S3 ---"
ensure_s3cfg
if ! $S3 info "s3://$S3_BUCKET" >/dev/null 2>&1; then
  $S3 mb "s3://$S3_BUCKET" >/dev/null 2>&1 || { echo "ERROR: cannot access/create bucket $S3_BUCKET"; exit 1; }
fi
if ! $S3 --no-progress put "$LOCAL_BACKUP" "s3://$S3_BUCKET/$S3_PREFIX/df-$DATE.tar.gz" >/dev/null 2>&1; then
  echo "ERROR: S3 upload failed"
  exit 1
fi
echo "  Uploaded: s3://$S3_BUCKET/$S3_PREFIX/df-$DATE.tar.gz"

echo "--- Step 3: Retention (keep last $RETENTION) ---"
$S3 ls "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | awk '{print $NF}' | grep '\.tar\.gz$' | sort -r | tail -n +$((RETENTION + 1)) | while read -r key; do
  [ -n "$key" ] || continue
  echo "  Removing old: $key"
  $S3 --no-progress del "$key" >/dev/null 2>&1 || true
done

rm -f "$LOCAL_BACKUP"

echo ""
echo "=== backup-s3 complete ==="
echo "  S3: s3://$S3_BUCKET/$S3_PREFIX/df-$DATE.tar.gz"
echo "  Restore: bash restore-s3.sh"
