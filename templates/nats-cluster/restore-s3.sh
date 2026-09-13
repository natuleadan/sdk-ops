#!/bin/bash
# nats-cluster restore-s3 — node-side DR restore: download the sealed stream
# backup from S3, unseal with the operator recipient NKey, stream restore via
# port-forward. Mirrors nats-dockerized/restore.sh (CLI 0.4.0 forms).
# Usage: restore-s3.sh <stream> [timestamp]
set -u

NS="${NATS_K8S_NAMESPACE:-nats}"
REL="${NATS_K8S_RELEASE:-nats}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/nats"
: "${S3_BUCKET:?}" : "${NATS_SEAL_SENDER_NK:?}" : "${NATS_UNSEAL_RECIPIENT_NK:?}"
: "${NATS_S3_PREFIX:=nats}"
S3_PREFIX="$NATS_S3_PREFIX"
PF_PORT="14222"

fail() { echo "restore: $1"; cleanup; exit 1; }
cleanup() { [ -n "${PF_PID:-}" ] && kill "$PF_PID" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT
WORK="/tmp/nats-restore"
rm -rf "$WORK"; mkdir -p "$WORK"

STREAM="${1:?usage: restore-s3.sh <stream> [timestamp]}"
TS="${2:-latest}"

[ -x "$BIN" ] || fail "node nats CLI missing at $BIN"

$KUBECTL -n "$NS" port-forward "svc/${REL}" "${PF_PORT}:4222" >/dev/null 2>&1 &
PF_PID=$!
URL="nats://127.0.0.1:${PF_PORT}"

S3CMD_CFG="$WORK/s3cfg"
cat > "$S3CMD_CFG" <<EOF2
[default]
access_key = ${S3_ACCESS_KEY:-}
secret_key = ${S3_SECRET_KEY:-}
host_base = $(echo "$S3_ENDPOINT" | sed 's#https\?://##; s#/*$##')
host_bucket = $(echo "$S3_ENDPOINT" | sed 's#https\?://##; s#/*$##')/${S3_BUCKET}
use_https = true
EOF2

if [ "$TS" = "latest" ]; then
  TS="$(s3cmd -c "$S3CMD_CFG" ls "s3://$S3_BUCKET/$S3_PREFIX/$STREAM/" 2>/dev/null | awk '{print $NF}' | sort | tail -1 | xargs -r basename 2>/dev/null | sed 's/\.nkey$//')"
  [ -n "$TS" ] || fail "no backup for stream $STREAM"
fi
KEY="s3://$S3_BUCKET/$S3_PREFIX/$STREAM/$TS.nkey"

SENDER_PUB="$("$BIN" auth nkey show "$NATS_SEAL_SENDER_NK" 2>/dev/null || true)"
[ -n "$SENDER_PUB" ] || fail "cannot derive sender pubkey from NATS_SEAL_SENDER_NK"

s3cmd -c "$S3CMD_CFG" get "$KEY" "$WORK/backup.nkey" --force >/dev/null 2>&1 || fail "download $KEY"
"$BIN" auth nkey unseal "$WORK/backup.nkey" "$NATS_UNSEAL_RECIPIENT_NK" "$SENDER_PUB" --output "$WORK/backup.tar.gz" >/dev/null 2>&1 || fail "unseal"
mkdir -p "$WORK/data"
tar xzf "$WORK/backup.tar.gz" -C "$WORK/data" || fail "extract"
snap="$(find "$WORK/data" -mindepth 1 -maxdepth 1 -type d | head -1)"
[ -n "$snap" ] || fail "empty backup archive"
ok=""
for i in $(seq 1 20); do
  if "$BIN" stream restore "$snap" --server="$URL" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
if [ -n "$ok" ]; then
  echo "restore: $STREAM restored from $TS"
else
  fail "stream restore"
fi
