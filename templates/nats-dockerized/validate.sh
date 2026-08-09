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

# Cert expiry: fail when < 30 days left (the renewal timer refreshes it).
CERT="$DIR/certs/server.pem"
if [ -f "$CERT" ]; then
  if openssl x509 -in "$CERT" -noout -checkend 2592000 >/dev/null 2>&1; then
    echo "  [PASS] server cert > 30d"
  else
    echo "  [FAIL] server cert expira en <30 dias"
    FAILED=1
  fi
else
  echo "  [FAIL] server cert ausente"
  FAILED=1
fi

# Host memory: fail when < 200MB free.
avail="$(free -m 2>/dev/null | awk '/^Mem:/{print $7}')"
if [ -n "$avail" ]; then
  if [ "$avail" -gt 200 ]; then
    echo "  [PASS] host free ${avail}MB"
  else
    echo "  [FAIL] memoria baja (${avail}MB)"
    FAILED=1
  fi
fi

if [ "$FAILED" -ne 0 ]; then echo "[nats-validate] FAILED"; exit 1; fi
echo "[nats-validate] OK"
exit 0
