#!/bin/bash
# nats-dockerized restore — unseal a stream backup from S3 and restore it.
# Runs from the operator side (the RECIPIENT NKey lives there). Usage:
#   restore.sh <stream> [timestamp]     # timestamp optional (default latest)
set -eu

DIR="/opt/sdk-ops/services/nats"
BIN="$DIR/nats"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"

: "${S3_BUCKET:?}" : "${NATS_URL:=tls://127.0.0.1:4222}"
: "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
: "${NATS_UNSEAL_RECIPIENT_NK:?}"   # operator-side path to the recipient NKey
: "${S3_PREFIX:=nats}"

STREAM="${1:?usage: restore.sh <stream> [timestamp]}"
TS="${2:-latest}"
WORK="/tmp/nats-restore"
rm -rf "$WORK"; mkdir -p "$WORK"

# Pick the latest .nkey for this stream unless a timestamp was given.
if [ "$TS" = "latest" ]; then
  TS="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/$STREAM/" 2>/dev/null | awk '{print $NF}' | tail -1 | xargs basename 2>/dev/null | sed 's/\.nkey$//')"
  [ -n "$TS" ] || { echo "restore: no backup for stream $STREAM"; exit 1; }
fi
KEY="s3://$S3_BUCKET/$S3_PREFIX/$STREAM/$TS.nkey"

s3cmd get "$KEY" "$WORK/backup.nkey" >/dev/null 2>&1 || { echo "restore: download failed"; exit 1; }
"$BIN" auth nkey unseal "$WORK/backup.nkey" "$NATS_UNSEAL_RECIPIENT_NK" --output "$WORK/backup.tar.gz" >/dev/null 2>&1 || { echo "restore: unseal failed"; exit 1; }
mkdir -p "$WORK/data"
tar xzf "$WORK/backup.tar.gz" -C "$WORK/data"
rm -f "$WORK/backup.tar.gz"
find "$WORK/data" -mindepth 2 -maxdepth 2 -type d -exec basename {} \; | while read -r snap; do
  "$BIN" stream restore "$STREAM" "$WORK/data/$STREAM/$snap" --force --user "$NATS_USER" --password "$NATS_PASSWORD" >/dev/null 2>&1 \
    && echo "restore: $STREAM restored from $snap" || echo "restore: skipped $snap"
done
rm -rf "$WORK"
