#!/bin/bash
# crowdsec-dockerized test - end-to-end L7 enforcement through the host
# Traefik: ban the client on the LAPI, assert Traefik answers 403 (the plugin
# middleware on the web entrypoint), then unban and assert it goes back to the
# normal response (404 from the probe's catch-all backend). Uses HTTP (:80)
# because websecure has no certificate for the probe host.
#
# The client IP Traefik observes depends on the network mode: bridged Traefik
# sees the docker gateway (port publishing SNATs the source), host-network
# Traefik sees 127.0.0.1. Both are banned for the probe. In client mode the
# decision must be added on the central (the local account has no admin rights).
set -u

DIR="${CS_DIR:-/opt/sdk-ops/services/crowdsec-dockerized}"
PROBE_HOST="${PROBE_HOST:-waf-probe.invalid}"
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"
MODE="standalone"; [ -n "${CS_LAPI_URL:-}" ] && MODE="client"

# Client addresses to ban for the probe.
PROBE_IPS="127.0.0.1"
GW="$(ip -4 -o addr show docker0 2>/dev/null | awk '{print $4}' | cut -d/ -f1 || true)"
if [ -n "$GW" ] && [ "$GW" != "127.0.0.1" ]; then PROBE_IPS="$PROBE_IPS $GW"; fi

FAILED=0
code_now() {
  curl -s -o /dev/null -w '%{http_code}' --max-time 6 -H "Host: $PROBE_HOST" http://127.0.0.1/ 2>/dev/null
}

echo "=== crowdsec-dockerized test (mode=$MODE, probes=$PROBE_IPS) ==="

banned=0
if sudo docker exec crowdsec cscli decisions add --ip 127.0.0.1 --duration 2m --reason "sdk-ops e2e" >/dev/null 2>&1; then
  banned=1
  for ip in $PROBE_IPS; do
    sudo docker exec crowdsec cscli decisions add --ip "$ip" --duration 2m --reason "sdk-ops e2e" >/dev/null 2>&1 || true
  done
  echo "  [PASS] decision added via cscli"
else
  echo "  [SKIP] cannot add a decision here (client mode: add it on the central)"
fi

if [ "$banned" = 1 ]; then
  got=000
  for i in $(seq 1 15); do
    got="$(code_now)"
    [ "$got" = "403" ] && break
    sleep 2
  done
  if [ "$got" = "403" ]; then
    echo "  [PASS] Traefik returns 403 for the banned client"
  else
    echo "  [FAIL] expected 403, got $got (plugin not enforcing?)"
    FAILED=1
  fi
  for ip in $PROBE_IPS; do
    sudo docker exec crowdsec cscli decisions delete --ip "$ip" >/dev/null 2>&1 || true
  done
  echo "  [INFO] decisions cleaned up"
  # The plugin refreshes stream decisions every 15s: poll until the ban lifts.
  after=403
  for i in $(seq 1 15); do
    after="$(code_now)"
    [ "$after" != "403" ] && break
    sleep 2
  done
  if [ "$after" != "403" ] && [ "$after" != "000" ]; then
    echo "  [PASS] unban restores normal responses ($after)"
  else
    echo "  [FAIL] unexpected response after unban: $after"
    FAILED=1
  fi
else
  if sudo grep -q "crowdsec@file" /etc/traefik/traefik.yml 2>/dev/null; then
    echo "  [PASS] plugin wired in traefik.yml"
  else
    echo "  [FAIL] plugin wiring missing from traefik.yml"
    FAILED=1
  fi
fi

if [ "$FAILED" -ne 0 ]; then echo "=== test: FAILED ==="; exit 1; fi
echo "=== test: OK ==="
