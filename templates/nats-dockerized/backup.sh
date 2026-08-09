#!/bin/bash
# nats-dockerized backup — seal JetStream streams to S3 with a NATS NKey (curve).
# Runs on the node (systemd timer). Reads its secrets from the service .env.
# Seal with the SENDER key on this host; only the RECIPIENT key (operator) can unseal.
set -u

DIR="/opt/sdk-ops/services/nats"
BIN="$DIR/nats"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"

: "${S3_BUCKET:?}" : "${NATS_URL:=tls://127.0.0.1:4222}"
: "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
: "${NATS_SEAL_SENDER_NK:?}" : "${NATS_SEAL_RECIPIENT_PUB:?}"
BACKUP_ROOT="/opt/backups"
RETENTION_DAYS=30
STAMP="$(date +%F)"
TS="$(date +%Y%m%d-%H%M%S)"
: "${S3_PREFIX:=nats}"

APP=(--server "$NATS_URL" --tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")

fail() { echo "[nats-backup] FAIL: $1"; exit 1; }
seal() { "$BIN" auth nkey seal "$1" "$NATS_SEAL_SENDER_NK" "$(cat "$NATS_SEAL_RECIPIENT_PUB")" --output "$2" >/dev/null 2>&1; }

mkdir -p "$BACKUP_ROOT" || { sudo mkdir -p "$BACKUP_ROOT" && sudo chown sdkops:sdkops "$BACKUP_ROOT"; } 2>/dev/null
streams="$("$BIN" stream ls --names "${APP[@]}" 2>/dev/null)"
[ -n "$streams" ] || fail "no streams"

for s in $streams; do
  dest="$BACKUP_ROOT/$s/$STAMP"
  sealed="$BACKUP_ROOT/$s/$STAMP.nkey"
  mkdir -p "$dest"
  "$BIN" stream backup "$s" "$dest" --consumers "${APP[@]}" >/dev/null 2>&1 || fail "backup $s"
  tar czf "$BACKUP_ROOT/$s/$STAMP.tar.gz" -C "$BACKUP_ROOT/$s" "$STAMP" || fail "tar $s"
  seal "$BACKUP_ROOT/$s/$STAMP.tar.gz" "$sealed" || fail "seal $s"
  rm -f "$BACKUP_ROOT/$s/$STAMP.tar.gz"; rm -rf "$dest"
  s3cmd put "$sealed" "s3://$S3_BUCKET/$S3_PREFIX/$s/$TS.nkey" >/dev/null 2>&1 || fail "s3 upload $s"
  echo "[nats-backup] $s -> s3://$S3_BUCKET/$S3_PREFIX/$s/$TS.nkey OK"
done

find "$BACKUP_ROOT" -name "*.nkey" -mtime +"$RETENTION_DAYS" -delete 2>/dev/null
cutoff="$(date -d "-$RETENTION_DAYS days" +%F)"
s3cmd ls -r "s3://$S3_BUCKET/$S3_PREFIX/" 2>/dev/null | while read -r line; do
  d="${line%% *}"; key="$(echo "$line" | awk '{print $NF}')"
  if [ "${d:-0}" \< "$cutoff" ]; then s3cmd del "$key" >/dev/null 2>&1; fi
done

# Identity bundle: node config, certs, keys, s3cfg — so a wiped node can be
# rebuilt from S3 (the recipient NKey on the operator decrypts it).
IDENT="$BACKUP_ROOT/identity-$TS.tar.gz"
sudo tar czf "$IDENT" -C / "$DIR" /home/sdkops/.s3cfg 2>/dev/null || fail "tar identity"
seal "$IDENT" "$BACKUP_ROOT/identity-$TS.nkey" || fail "seal identity"
rm -f "$IDENT"
s3cmd put "$BACKUP_ROOT/identity-$TS.nkey" "s3://$S3_BUCKET/$S3_PREFIX/identity/$TS.nkey" >/dev/null 2>&1 || fail "s3 identity"
rm -f "$BACKUP_ROOT/identity-$TS.nkey"
echo "[nats-backup] identity -> s3://$S3_BUCKET/$S3_PREFIX/identity/$TS.nkey OK"

echo "[nats-backup] OK"
exit 0
