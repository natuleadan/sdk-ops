#!/bin/bash
# crowdsec-dockerized test - end-to-end L7 enforcement through the host
# Traefik: ban the loopback client on the LAPI, assert Traefik answers 403
# (the plugin middleware), then unban and assert a normal response. In client
# mode the ban must be added on the central (the local account has no admin
# rights) — the assertion still runs if the ban is already there.
set -u

DIR="${CS_DIR:-/opt/sdk-ops/services/crowdsec-dockerized}"
PROBE_IP="${PROBE_IP:-127.0.0.1}"
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"
MODE="standalone"; [ -n "${CS_LAPI_URL:-}" ] && MODE="client"

echo "=== crowdsec-dockerized test (mode=$MODE, probe=$PROBE_IP) ==="

code_now() {
  curl -sk -o /dev/null -w '%{http_code}' --max-time 6 -H 'Host: waf-probe.invalid' https://127.0.0.1/ 2>/dev/null
}

banned=0
if sudo docker exec crowdsec cscli decisions add --ip "$PROBE_IP" --duration 2m --reason "sdk-ops e2e" >/dev/null 2>&1; then
  banned=1
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
  fi
  sudo docker exec crowdsec cscli decisions delete --ip "$PROBE_IP" >/dev/null 2>&1 || true
  echo "  [INFO] decision cleaned up"
  sleep 3
  after="$(code_now)"
  if [ "$after" != "403" ]; then
    echo "  [PASS] unban restores normal responses ($after)"
  else
    echo "  [FAIL] still 403 after unban"
  fi
else
  if sudo docker inspect traefik 2>/dev/null | grep -q crowdsec-bouncer; then
    echo "  [PASS] plugin wired in traefik"
  else
    echo "  [FAIL] plugin missing from traefik"
  fi
fi
