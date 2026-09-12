#!/bin/bash
# nats-bare validate - assert this cluster node is healthy (native nats-server
# + systemd, no Docker: same checks as nats-dockerized, process/systemd
# instead of container names).
set -u

DIR="${NATS_DIR:-/opt/sdk-ops/services/nats-bare}"
BIN="${BIN:-/usr/local/bin/nats}"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"

: "${NATS_URL:=tls://127.0.0.1:4222}" : "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
APP=(--server "$NATS_URL" --tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")

FAILED=0
check() { if "$BIN" "$@" "${APP[@]}" --format=nagios >/dev/null 2>&1; then echo "  [PASS] $*"; else echo "  [FAIL] $*"; FAILED=1; fi; }

echo "[nats-validate] $NATS_URL"

# The native unit must be active (the dockerized check was the container).
if systemctl is-active --quiet nats-server 2>/dev/null; then
  echo "  [PASS] systemd unit nats-server active"
else
  echo "  [FAIL] systemd unit nats-server not active"
  FAILED=1
fi

check server check connection
check server check jetstream
# App streams (e.g. events) belong to the microservices, not the server —
# their absence is informational, never a server failure.
if "$BIN" stream info events "${APP[@]}" >/dev/null 2>&1; then
  echo "  [PASS] stream info events"
else
  echo "  [INFO] no app stream 'events' yet (created by the microservices)"
fi

# Cluster awareness: report the peer count from the rendered nats.conf
# `routes:` list (each route is one peer). NATS_ROUTES/NATS_PEERS env
# overrides when present (comma-separated). The app account cannot query
# server info (SYS-only), so the config is the source of truth here.
CONF="$DIR/nats.conf"
PEERS=""
if [ -n "${NATS_ROUTES:-}" ]; then
  PEERS="$NATS_ROUTES"
elif [ -n "${NATS_PEERS:-}" ]; then
  PEERS="$NATS_PEERS"
elif [ -f "$CONF" ]; then
  PEERS="$(grep -oE 'nats://(\[[0-9a-fA-F:]+\]|[0-9A-Za-z.-]+)' "$CONF" 2>/dev/null | sed 's#nats://##; s/^\[//; s/\]$//' | tr '\n' ',' | sed 's/,$//')"
fi
if [ -n "$PEERS" ]; then
  n=$(echo "$PEERS" | tr ',' '\n' | grep -c .)
  echo "  [PASS] peers: $n ($(echo "$PEERS" | tr ',' ' '))"
else
  echo "  [SKIP] peers (single node)"
fi

# `nats server check cluster` does not exist in the pinned CLI 0.4.0
# (subcommands: connection stream consumer message meta request jetstream
# server kv credential) - SKIP it defensively instead of failing.
if "$BIN" server check cluster "${APP[@]}" --format=nagios >/dev/null 2>&1; then
  echo "  [PASS] server check cluster"
else
  if "$BIN" server check --help 2>&1 | grep -q "check cluster"; then
    echo "  [FAIL] server check cluster"
    FAILED=1
  else
    echo "  [SKIP] server check cluster (not in this CLI version)"
  fi
fi

# JetStream cluster signal visible to the app account: `server check jetstream`
# reports replicas_ok=N from the account's streams.
js="$("$BIN" server check jetstream "${APP[@]}" 2>&1)"
reps="$(echo "$js" | grep -oE 'replicas_ok=[0-9]+' | head -1)"
if [ -n "$reps" ]; then
  echo "  [PASS] $reps"
fi

# Cert expiry: fail when < 30 days left (gen-certs.sh issues 825-day certs;
# regenerate or re-run gen-certs.sh to refresh).
CERT="$DIR/certs/server.pem"
if [ -f "$CERT" ]; then
  if openssl x509 -in "$CERT" -noout -checkend 2592000 >/dev/null 2>&1; then
    echo "  [PASS] server cert > 30d"
  else
    echo "  [FAIL] server cert expires in <30 days"
    FAILED=1
  fi
else
  echo "  [FAIL] server cert missing"
  FAILED=1
fi

# Host memory: fail when < 200MB free.
avail="$(free -m 2>/dev/null | awk '/^Mem:/{print $7}')"
if [ -n "$avail" ]; then
  if [ "$avail" -gt 200 ]; then
    echo "  [PASS] host free ${avail}MB"
  else
    echo "  [FAIL] low host memory (${avail}MB)"
    FAILED=1
  fi
fi

if [ "$FAILED" -ne 0 ]; then echo "[nats-validate] FAILED"; exit 1; fi
echo "[nats-validate] OK"
exit 0
