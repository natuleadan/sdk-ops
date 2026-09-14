#!/bin/bash
# crowdsec-dockerized validate - engine container, Traefik plugin wiring and
# the access-log shared with the file provider. In client mode the LAPI checks
# run against the REMOTE central and the bouncer listing is informational.
set -u

DIR="${CS_DIR:-/opt/sdk-ops/services/crowdsec-dockerized}"
BOUNCER="${CS_BOUNCER_NAME:-{{ .BouncerName }}}"
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"

MODE="standalone"
[ -n "${CS_LAPI_URL:-}" ] && MODE="client (${CS_LAPI_URL})"

FAILED=0
ok()   { echo "  [PASS] $1"; }
bad()  { echo "  [FAIL] $1"; FAILED=1; }
info() { echo "  [INFO] $1"; }

echo "=== crowdsec-dockerized validate ($MODE) ==="

if [ -n "$(sudo docker ps -q -f name=crowdsec)" ]; then
  ok "engine container running"
else
  bad "engine container not running"
fi

if [ -n "$(sudo docker ps -q -f name=traefik)" ]; then
  ok "traefik container running"
else
  bad "traefik container not running"
fi

if sudo grep -q "crowdsec-bouncer" /etc/traefik/traefik.yml 2>/dev/null \
   && sudo grep -q "crowdsec@file" /etc/traefik/traefik.yml 2>/dev/null; then
  ok "traefik.yml wires the plugin + entrypoint middleware"
else
  bad "traefik.yml missing the plugin/entrypoint middleware wiring"
fi

if [ -f /etc/traefik/conf.d/01-crowdsec.yml ]; then
  ok "middleware file in the file provider"
else
  bad "middleware file /etc/traefik/conf.d/01-crowdsec.yml missing"
fi

if sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1; then
  ok "lapi status (${CS_LAPI_URL:-local})"
else
  bad "lapi status failed (${CS_LAPI_URL:-local})"
fi

if sudo docker exec crowdsec cscli bouncers list -o json 2>/dev/null | grep -q "\"$BOUNCER\""; then
  ok "bouncer $BOUNCER registered"
elif [ "$MODE" != "standalone" ]; then
  info "bouncer listing needs central admin rights (client mode)"
else
  bad "bouncer $BOUNCER not registered"
fi

if [ -d /var/log/traefik ]; then
  ok "shared access-log dir present"
else
  bad "/var/log/traefik missing (access log cannot be parsed)"
fi

if sudo docker logs traefik --tail=400 2>&1 | grep -qi "Plugins are disabled"; then
  bad "traefik reports plugins disabled (download failed — check egress 443)"
else
  ok "traefik plugin state ok"
fi

if [ "$FAILED" -ne 0 ]; then echo "=== validate: FAILED ==="; exit 1; fi
echo "=== validate: OK ==="
