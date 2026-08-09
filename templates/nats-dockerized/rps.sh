#!/bin/bash
# nats-dockerized rps — NATS throughput (core pub or JetStream sync).
# Usage: ./rps.sh [--js] [--rps N] [--conns N]
set -eu

DIR="/opt/sdk-ops/services/nats"
BIN="$DIR/nats"
ENV="$DIR/.env"
[ -f "$ENV" ] && . "$ENV"
: "${NATS_URL:=tls://127.0.0.1:4222}" : "${NATS_USER:=app}" : "${NATS_PASSWORD:?}"
APP=(--server "$NATS_URL" --tlsfirst --tlsca "$DIR/certs/ca.pem" --tlscert "$DIR/certs/app-cert.pem" --tlskey "$DIR/certs/app-key.pem" --user "$NATS_USER" --password "$NATS_PASSWORD")

JS=false
RPS=500000
CONNS=100
case "$1" in
  --js) JS=true; RPS=100000; CONNS=20 ;;
esac

if $JS; then
  exec "$BIN" bench demo.bench --js --pub "$CONNS" --size 128 --msgs "$RPS" "${APP[@]}"
fi
exec "$BIN" bench demo.bench --pub "$CONNS" --size 128 --msgs "$RPS" "${APP[@]}"
