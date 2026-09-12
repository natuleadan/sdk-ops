#!/bin/bash
# nats-cluster backup-s3 — node-side DR backup: stream backup via port-forward,
# tar, NKey seal (curve), upload to S3. Mirrors the nats-dockerized backup
# flow; only the transport differs (port-forward instead of localhost).
# Requires on this node: the pinned nats CLI (created by test.sh or manually),
# s3cmd, and env: S3_*, NATS_SEAL_SENDER_NK, NATS_SEAL_RECIPIENT_PUB.
# Usage: backup-s3.sh [stream]        # default: every stream
set -u

NS="${NATS_K8S_NAMESPACE:-nats}"
REL="${NATS_K8S_RELEASE:-nats}"
KUBECTL="k3s kubectl"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/nats"
: "${S3_BUCKET:?}" : "${NATS_SEAL_SENDER_NK:?}" : "${NATS_SEAL_RECIPIENT_PUB:?}"
: "${S3_PREFIX:=nats}"
TS="$(date +%Y%m%d-%H%M%S)"
PF_PORT="14222"

fail() { echo "[nats-backup] FAIL: $1"; cleanup; exit 1; }
cleanup() { [ -n "${PF_PID:-}" ] && kill "$PF_PID" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT
WORK="/tmp/nats-backup"
rm -rf "$WORK"; mkdir -p "$WORK"

[ -x "$BIN" ] || fail "node nats CLI missing at $BIN (run it once or download pinned ${NATS_CLI_VERSION:-0.4.0})"

$KUBECTL -n "$NS" port-forward "svc/${REL}" "${PF_PORT}:4222" >/dev/null 2>&1 &
PF_PID=$!
URL="nats://127.0.0.1:${PF_PORT}"

S3CMD_CFG="$WORK/s3cfg"
cat > "$S3CMD_CFG" <<EOF2
[default]
access_key = ${S3_ACCESS_KEY:-}
secret_key = ${S3_SECRET_KEY:-}
host_base = $(echo "$S3_ENDPOINT" | sed 's#https\?://##')
host_bucket = $(echo "$S3_ENDPOINT" | sed 's#https\?://##')/${S3_BUCKET}
use_https = true
EOF2

streams=""
for i in $(seq 1 20); do
  streams="$("$BIN" stream ls --names --server "$URL" 2>/dev/null)"
  [ -n "$streams" ] && break
  sleep 1
done
[ -n "$streams" ] || fail "no streams"
for s in $streams; do
  rm -rf "$WORK/$s"; mkdir -p "$WORK/$s"
  "$BIN" stream backup "$s" "$WORK/$s/$s" --server "$URL" >/dev/null 2>&1 || fail "backup $s"
  tar czf "$WORK/$s.tar.gz" -C "$WORK/$s" "$s" || fail "tar $s"
  "$BIN" auth nkey seal "$WORK/$s.tar.gz" "$NATS_SEAL_SENDER_NK" "$(cat "$NATS_SEAL_RECIPIENT_PUB")" --output "$WORK/$s.nkey" >/dev/null 2>&1 || fail "seal $s"
  s3cmd -c "$S3CMD_CFG" put "$WORK/$s.nkey" "s3://$S3_BUCKET/$S3_PREFIX/$s/$TS.nkey" >/dev/null 2>&1 || fail "s3 upload $s"
  echo "[nats-backup] $s -> s3://$S3_BUCKET/$S3_PREFIX/$s/$TS.nkey OK"
done
echo "[nats-backup] OK"
