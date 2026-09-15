#!/bin/bash
# crowdsec-dockerized test - end-to-end L7 enforcement through the host
# Traefik. Phase 1: ban the client on the LAPI, assert Traefik answers 403
# (the plugin middleware on the web entrypoint), then unban and assert it goes
# back to the normal response (404 from the probe's catch-all backend).
# Phase 2 (standalone only): automatic detection - the whitelist parser is
# removed (node-local traffic is loopback/RFC1918, always whitelisted), a scan
# of DISTINCT sensitive paths must raise a crowdsec-kind alert and auto-ban
# (403), then everything is cleaned up and the whitelist restored.
#
# Phase 3 (AppSec profiles only): in-band CRS - SQLi 403s first try, benign
# stays normal. Uses HTTP (:80) because websecure has no certificate for the
# probe host.
#
# The client IP Traefik observes depends on the network mode: bridged Traefik
# sees the docker gateway (port publishing SNATs the source), host-network
# Traefik sees 127.0.0.1. Both are banned for the probe. In client mode the
# decision must be added on the central (the local account has no admin rights).
set -u

DIR="${CS_DIR:-/opt/sdk-ops/services/crowdsec-dockerized}"
PROBE_HOST="${PROBE_HOST:-waf-probe.invalid}"
APPSEC="{{ .AppSec }}"
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

  # Phase 2 - automatic detection. CrowdSec whitelists loopback/RFC1918 by
  # default and node-local traffic always comes from there (127.0.0.1 on
  # host-network Traefik, the docker gateway on bridged), so the whitelist
  # parser is removed for the scan and restored right after (trap-guarded,
  # plus validate.sh fails when it is missing). The scenarios count DISTINCT
  # requests, so hammering one URL never overflows.
  # NOTE: reruns within ~5m of a detection hit the scenario blackhole and
  # will not re-fire; wait it out before re-running the test.
  restore_wl() {
    sudo docker exec crowdsec cscli parsers install crowdsecurity/whitelists >/dev/null 2>&1 || true
    sudo docker restart crowdsec >/dev/null 2>&1 || true
    for i in $(seq 1 30); do
      if sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1; then break; fi
      sleep 2
    done
  }
  trap restore_wl EXIT
  echo "  [INFO] automatic-detection phase (whitelist off, distinct scan)"
  sudo docker exec crowdsec cscli parsers remove crowdsecurity/whitelists >/dev/null 2>&1 || true
  sudo docker restart crowdsec >/dev/null
  for i in $(seq 1 30); do
    if sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1; then break; fi
    sleep 2
  done
  sudo docker exec crowdsec cscli lapi status >/dev/null 2>&1 || { echo "  [FAIL] engine did not come back after whitelist removal"; FAILED=1; }
  # Settle: the file tailer starts seconds after the LAPI answers, and lines
  # written before it tails are missed. (cscli metrics omits idle sources, so
  # it cannot gate this - a fixed settle is the deterministic option.)
  sleep 10
  for ip in $PROBE_IPS; do
    sudo docker exec crowdsec cscli decisions delete --ip "$ip" >/dev/null 2>&1 || true
  done
  for p in .git/config .env wp-config.php config.php server-status phpinfo.php "admin/.git/config" backup.sql db.sqlite .svn/entries .git/HEAD composer.json .DS_Store server-info web.config database.sql dump.sql .htpasswd actuator/env change-password; do
    curl -s -o /dev/null --max-time 5 -H "Host: $PROBE_HOST" "http://127.0.0.1/$p" >/dev/null 2>&1 || true
  done
  echo "  [INFO] scan sent, polling for an automatic alert on the probe source"
  WL_PAT="$(printf '%s' "$PROBE_IPS" | tr ' ' '|' | sed 's/\./\\./g')"
  found=""
  for i in $(seq 1 24); do
    if sudo docker exec crowdsec cscli alerts list --since 4m 2>/dev/null | grep -Eq "($WL_PAT).*crowdsec"; then found=1; break; fi
    sleep 5
  done
  if [ -n "$found" ]; then
    echo "  [PASS] engine auto-detected the scan (crowdsec-kind alert)"
  else
    echo "  [FAIL] no automatic alert after the scan (parser/scenarios?)"
    FAILED=1
  fi
  got=000
  for i in $(seq 1 15); do
    got="$(code_now)"
    [ "$got" = "403" ] && break
    sleep 2
  done
  if [ "$got" = "403" ]; then
    echo "  [PASS] auto-ban enforced by Traefik (403)"
  else
    echo "  [FAIL] expected 403 from the auto-ban, got $got"
    FAILED=1
  fi
  for ip in $PROBE_IPS; do
    sudo docker exec crowdsec cscli decisions delete --ip "$ip" >/dev/null 2>&1 || true
  done
  echo "  [INFO] auto-ban cleaned up, restoring the whitelist"
  trap - EXIT
  restore_wl
  if sudo docker exec crowdsec cscli parsers list 2>/dev/null | grep -q "crowdsecurity/whitelists"; then
    echo "  [PASS] whitelist parser restored"
  else
    echo "  [FAIL] whitelist parser NOT restored (engine left weakened)"
    FAILED=1
  fi
  after=403
  for i in $(seq 1 15); do
    after="$(code_now)"
    [ "$after" != "403" ] && break
    sleep 2
  done
  if [ "$after" != "403" ] && [ "$after" != "000" ]; then
    echo "  [PASS] post-cleanup responses normal ($after)"
  else
    echo "  [FAIL] still $after after cleanup"
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

if [ "$APPSEC" = "true" ]; then
  # Phase 3 - in-band WAF: a SQLi probe must 403 on the first try (no ban,
  # no stream wait - the AppSec server answers synchronously), then a benign
  # request stays normal. Out-of-band scenarios may still ban the probe source
  # as a side effect, so decisions are cleared before the benign check.
  echo "  [INFO] appsec phase (in-band CRS: first-try block, no ban needed)"
  for ip in $PROBE_IPS; do
    sudo docker exec crowdsec cscli decisions delete --ip "$ip" >/dev/null 2>&1 || true
  done
  got=000
  for i in $(seq 1 5); do
    got="$(curl -s -o /dev/null -w '%{http_code}' --max-time 6 -H "Host: $PROBE_HOST" "http://127.0.0.1/?id=1%27%20UNION%20SELECT%201,2,3--" 2>/dev/null || echo 000)"
    [ "$got" = "403" ] && break
    sleep 3
  done
  if [ "$got" = "403" ]; then
    echo "  [PASS] appsec blocked the SQLi in-band (403 first try)"
  else
    echo "  [FAIL] expected in-band 403 for the SQLi probe, got $got"
    FAILED=1
  fi
  for ip in $PROBE_IPS; do
    sudo docker exec crowdsec cscli decisions delete --ip "$ip" >/dev/null 2>&1 || true
  done
  benign=403
  for i in $(seq 1 15); do
    benign="$(curl -s -o /dev/null -w '%{http_code}' --max-time 6 -H "Host: $PROBE_HOST" "http://127.0.0.1/" 2>/dev/null || echo 000)"
    [ "$benign" != "403" ] && break
    sleep 2
  done
  if [ "$benign" != "403" ] && [ "$benign" != "000" ]; then
    echo "  [PASS] benign requests stay normal ($benign)"
  else
    echo "  [FAIL] benign request got $benign"
    FAILED=1
  fi
fi

if [ "$FAILED" -ne 0 ]; then echo "=== test: FAILED ==="; exit 1; fi
echo "=== test: OK ==="
