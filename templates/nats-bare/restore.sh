#!/bin/bash
# nats-bare restore - download a sealed stream backup from S3, unseal it
# and restore the stream. Port of the nats-dockerized restore (native paths,
# no Docker). CLI 0.4.0 forms (space-separated globals mis-parse):
#   unseal:  auth nkey unseal <blob.nkey> <recipient_nk> <sender_pub> --output <tar.gz>
#   restore: stream restore <dir> --flag=value
# Runs on the node (it holds the sender NKey and s3cmd); the RECIPIENT NKey is
# the operator secret - pass it via NATS_UNSEAL_RECIPIENT_NK when invoking.
# Usage:
#   restore.sh <stream> [timestamp]     # timestamp optional (default latest)
set -eu

DIR="/opt/sdk-ops/services/nats-bare"
BIN="/usr/local/bin/nats"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"

: "${S3_BUCKET:?}" : "${NATS_URL:=tls://127.0.0.1:4222}"
: "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
: "${NATS_SEAL_SENDER_NK:?}"        # sender NKey file on this node (mirrors backup.sh)
: "${NATS_UNSEAL_RECIPIENT_NK:?}"   # recipient NKey file (operator side - never committed)
: "${S3_PREFIX:=nats}"

STREAM="${1:?usage: restore.sh <stream> [timestamp]}"
TS="${2:-latest}"
WORK="/tmp/nats-restore"
rm -rf "$WORK"; mkdir -p "$WORK"

fail() { echo "restore: $1"; rm -rf "$WORK"; exit 1; }

# CLI 0.4.0 flag arrays: client flags space-separated, stream restore flags
# in the --flag=value form (see test/test.sh Step 6).
APP=(--server "$NATS_URL" --tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")
RAPP=(--server="$NATS_URL" --tlsfirst --tlsca="$DIR/certs/ca.pem" --tlscert="$DIR/certs/app-cert.pem" --tlskey="$DIR/certs/app-key.pem" --user="$NATS_USER" --password="$NATS_PASSWORD")

# Pick the latest .nkey for this stream unless a timestamp was given.
if [ "$TS" = "latest" ]; then
  TS="$(s3cmd ls "s3://$S3_BUCKET/$S3_PREFIX/$STREAM/" 2>/dev/null | awk '{print $NF}' | sort | tail -1 | xargs -r basename 2>/dev/null | sed 's/\.nkey$//')"
  [ -n "$TS" ] || fail "no backup for stream $STREAM"
fi
KEY="s3://$S3_BUCKET/$S3_PREFIX/$STREAM/$TS.nkey"

# The unseal sender public key derives from the sender NKey that stays on this
# node (the same key backup.sh sealed with) - auth nkey show prints it.
SENDER_PUB="$("$BIN" auth nkey show "$NATS_SEAL_SENDER_NK" 2>/dev/null || true)"
[ -n "$SENDER_PUB" ] || fail "cannot derive sender pubkey from NATS_SEAL_SENDER_NK"

s3cmd get "$KEY" "$WORK/backup.nkey" >/dev/null 2>&1 || fail "download $KEY"
"$BIN" auth nkey unseal "$WORK/backup.nkey" "$NATS_UNSEAL_RECIPIENT_NK" "$SENDER_PUB" --output "$WORK/backup.tar.gz" >/dev/null 2>&1 || fail "unseal"
mkdir -p "$WORK/data"
tar xzf "$WORK/backup.tar.gz" -C "$WORK/data" || fail "extract"
rm -f "$WORK/backup.nkey" "$WORK/backup.tar.gz"

# The tar holds one stream snapshot dir (the backup stamp) - restore it.
snap="$(find "$WORK/data" -mindepth 1 -maxdepth 1 -type d | head -1)"
[ -n "$snap" ] || fail "empty backup archive"
if "$BIN" stream restore "$snap" "${RAPP[@]}" >/dev/null 2>&1; then
  echo "restore: $STREAM restored from $TS"
else
  fail "stream restore"
fi
rm -rf "$WORK"
