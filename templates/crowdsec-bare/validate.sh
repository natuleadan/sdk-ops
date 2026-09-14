#!/bin/bash
# crowdsec-bare validate - assert the native engine and the nftables bouncer
# are up and wired. In client mode (CS_LAPI_URL) the LAPI checks run against
# the REMOTE central; the bouncer registration is informational there (a
# machine account cannot list bouncers on the central).
set -u

DIR="${CS_DIR:-/opt/sdk-ops/services/crowdsec-bare}"
# The bouncer is registered under the fleet host name (the OS hostname may be
# longer), rendered by the provision.
BOUNCER="${CS_BOUNCER_NAME:-{{ .BouncerName }}}"
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"

FAILED=0
ok()   { echo "  [PASS] $1"; }
bad()  { echo "  [FAIL] $1"; FAILED=1; }
info() { echo "  [INFO] $1"; }

MODE="standalone"
[ -n "${CS_LAPI_URL:-}" ] && MODE="client (${CS_LAPI_URL})"
echo "=== crowdsec-bare validate ($MODE) ==="

if systemctl is-active --quiet crowdsec; then ok "systemd unit crowdsec active"; else bad "systemd unit crowdsec not active"; fi
if systemctl is-active --quiet crowdsec-firewall-bouncer; then ok "systemd unit crowdsec-firewall-bouncer active"; else bad "bouncer unit not active"; fi

if command -v cscli >/dev/null 2>&1; then
  ok "cscli present ($(cscli version 2>/dev/null | awk '/version:/{print $2; exit}'))"
else
  bad "cscli missing (engine not installed)"
fi

if cscli lapi status >/dev/null 2>&1; then
  ok "lapi status (${CS_LAPI_URL:-local})"
else
  bad "lapi status failed (${CS_LAPI_URL:-local})"
fi

if cscli bouncers list -o json 2>/dev/null | grep -q "\"$BOUNCER\""; then
  ok "bouncer $BOUNCER registered"
elif [ "$MODE" != "standalone" ]; then
  info "bouncer listing needs central admin rights (client mode)"
else
  bad "bouncer $BOUNCER not registered"
fi

if [ -f /etc/crowdsec/acquis.yaml ] || compgen -G "/etc/crowdsec/acquis.d/*.yaml" >/dev/null; then
  ok "acquisition config present"
else
  info "no acquisition config (nothing to parse on this host)"
fi

if nft list ruleset 2>/dev/null | grep -qi crowdsec; then
  ok "nftables crowdsec set present"
else
  bad "no crowdsec table/set in nftables (bouncer not enforcing)"
fi

avail="$(free -m 2>/dev/null | awk '/^Mem:/{print $7}')"
if [ -n "$avail" ] && [ "$avail" -lt 128 ]; then
  bad "low host memory (${avail}MB free)"
else
  [ -n "$avail" ] && ok "host free ${avail}MB"
fi

if [ "$FAILED" -ne 0 ]; then echo "=== validate: FAILED ==="; exit 1; fi
echo "=== validate: OK ==="
