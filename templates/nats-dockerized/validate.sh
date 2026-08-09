#!/bin/bash
# nats-dockerized validate — assert this cluster node is healthy.
set -u

DIR="/opt/sdk-ops/services/nats"
BIN="$DIR/nats"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"

: "${NATS_URL:=tls://127.0.0.1:4222}" : "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
APP=(--server "$NATS_URL" --tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")

FAILED=0
check() { if "$BIN" "$@" "${APP[@]}" --format=nagios >/dev/null 2>&1; then echo "  [PASS] $*"; else echo "  [FAIL] $*"; FAILED=1; fi; }

echo "[nats-validate] $NATS_URL"
check server check connection
check server check jetstream
if "$BIN" stream info events "${APP[@]}" >/dev/null 2>&1; then
  echo "  [PASS] stream info events"
else
  echo "  [FAIL] stream info events"
  FAILED=1
fi

if [ "$FAILED" -ne 0 ]; then echo "[nats-validate] FAILED"; exit 1; fi
echo "[nats-validate] OK"
exit 0
