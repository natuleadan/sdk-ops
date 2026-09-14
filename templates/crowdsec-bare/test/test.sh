#!/bin/bash
# crowdsec-bare test - end-to-end enforcement proof without touching a real
# attacker: add a documented-range decision through cscli, wait for the
# bouncer to stream it into nftables, assert the IP is present, then clean
# up. In client mode the decision must be added on the central (the local
# machine account has no admin rights) — run `cscli decisions add` there and
# re-run this script; the bouncer assertion still holds locally.
set -u

DIR="${CS_DIR:-/opt/sdk-ops/services/crowdsec-bare}"
PROBE="${PROBE_IP:-198.51.100.66}"
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"
MODE="standalone"; [ -n "${CS_LAPI_URL:-}" ] && MODE="client"

echo "=== crowdsec-bare test (mode=$MODE, probe=$PROBE) ==="

added=0
if cscli decisions add --ip "$PROBE" --duration 2m --reason "sdk-ops e2e test" >/dev/null 2>&1; then
  added=1
  echo "  [PASS] decision added via cscli"
else
  echo "  [SKIP] cannot add a decision here (client mode: add it on the central)"
fi

if [ "$added" = 1 ]; then
  enforced=0
  for i in $(seq 1 15); do
    if nft list ruleset 2>/dev/null | grep -q "$PROBE"; then enforced=1; break; fi
    sleep 2
  done
  if [ "$enforced" = 1 ]; then
    echo "  [PASS] nftables enforces the decision ($PROBE)"
  else
    echo "  [FAIL] decision not enforced within 30s"
  fi
  cscli decisions delete --ip "$PROBE" >/dev/null 2>&1 || true
  echo "  [INFO] decision cleaned up"
else
  if nft list ruleset 2>/dev/null | grep -qi crowdsec; then
    echo "  [PASS] nftables crowdsec set is live (enforcement path present)"
  else
    echo "  [FAIL] no crowdsec set in nftables"
  fi
fi

if systemctl is-active --quiet crowdsec-firewall-bouncer; then
  echo "  [PASS] bouncer unit active"
else
  echo "  [FAIL] bouncer unit inactive"
fi
