#!/bin/bash
# crowdsec-bare init - install the CrowdSec engine + the nftables firewall
# bouncer natively under systemd (no Docker), from the PINNED apt repo
# (signed key + pinned release; no curl|sh). Two modes:
#   standalone (default) - local LAPI, bouncer pulls decisions from it.
#   client (CS_LAPI_URL) - the local agent reports to a REMOTE LAPI and the
#     local bouncer consumes that LAPI's decisions (the VLAN/multi-host
#     layout: one central engine processes, every host enforces locally).
# Idempotent: re-running converges the packages, the bouncer key/config and
# the units without wiping decisions.
set -e

DIR="/opt/sdk-ops/services/crowdsec-bare"
CS_VERSION="{{ .CSVersion }}"
BOUNCER_VERSION="{{ .BouncerVersion }}"
BOUNCER="{{ .BouncerName }}"
COLLECTIONS="{{ .Collections }}"
LAPI_LISTEN="{{ .LapiListen }}"
CLIENT="{{ if .Client }}1{{ else }}0{{ end }}"
MEM_LIMIT="{{ .MemLimit }}"
CPU_QUOTA="{{ .CpuQuota }}"

log() { echo "[crowdsec-bare] $1"; }
fail() { echo "[crowdsec-bare] FAIL: $1"; exit 1; }
# Secrets written by the provision with umask 077 (client mode: LAPI URL,
# machine credentials, bouncer key). Never rendered into this script.
# shellcheck disable=SC1091
[ -f "$DIR/.env" ] && . "$DIR/.env"

echo "=== crowdsec-bare init ==="
log "engine $CS_VERSION  bouncer $BOUNCER_VERSION  node $BOUNCER  client=$CLIENT"

# 1. Pinned apt repo: signed keyring + a fixed source line for this distro.
#    Replicates the official packagecloud setup without piping a remote
#    script into a shell.
command -v curl >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y -qq curl ca-certificates; }
command -v gpg >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y -qq gnupg; }
KEYRING=/etc/apt/keyrings/crowdsec_crowdsec-archive-keyring.gpg
if [ ! -s "$KEYRING" ]; then
  install -d -m 0755 /etc/apt/keyrings
  curl -fsSL https://packagecloud.io/crowdsec/crowdsec/gpgkey | gpg --dearmor > "$KEYRING"
  chmod 0644 "$KEYRING"
fi
# The repo uses the distro-independent `any/any` suite (upstream's current
# method): newer releases (e.g. Ubuntu 26.04 "resolute") have no per-release
# build, and the any suite carries the same pinned versions for every distro.
SOURCE=/etc/apt/sources.list.d/crowdsec_crowdsec.list
WANT_SOURCE="deb [signed-by=$KEYRING] https://packagecloud.io/crowdsec/crowdsec/any/ any main"
if [ ! -f "$SOURCE" ] || ! grep -qF "$WANT_SOURCE" "$SOURCE"; then
  printf '%s\n' "$WANT_SOURCE" > "$SOURCE"
  log "crowdsec apt repo pinned (any suite)"
fi
apt-get update -qq

# 2. Install the pinned engine + bouncer. The version prefix is resolved
#    against the repo (packagecloud may add a distro suffix); an absent
#    prefix fails loudly instead of installing a surprise version.
pin_install() {
  local pkg="$1" want="$2" avail have
  have="$(dpkg-query -W -f='${Version}' "$pkg" 2>/dev/null || true)"
  if [ -n "$have" ] && printf '%s' "$have" | grep -q "^${want}"; then
    log "$pkg already $have"
    return 0
  fi
  avail="$(apt-cache madison "$pkg" 2>/dev/null | awk -v w="$want" '$3 ~ "^"w {print $3; exit}')"
  [ -n "$avail" ] || fail "no $pkg matching $want (available: $(apt-cache madison "$pkg" 2>/dev/null | awk '{print $3}' | tr '\n' ' '))"
  log "installing $pkg=$avail"
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --allow-downgrades --allow-change-held-packages "$pkg=$avail"
}
pin_install crowdsec "$CS_VERSION"
pin_install crowdsec-firewall-bouncer-nftables "$BOUNCER_VERSION"

# 3. Hub collections (base detections). The package postinstall already
#    installs the detected set; this converges to the profile list.
for c in $COLLECTIONS; do
  cscli collections install "$c" >/dev/null 2>&1 || log "warn: collection $c not installed (hub unreachable?)"
done

# 4. Acquisition: parse the SSH log so brute-force scenarios work.
if [ -f /var/log/auth.log ] && [ ! -f /etc/crowdsec/acquis.d/sdkops-sshd.yaml ]; then
  mkdir -p /etc/crowdsec/acquis.d
  cat > /etc/crowdsec/acquis.d/sdkops-sshd.yaml <<'EOF'
source: file
filenames:
  - /var/log/auth.log
labels:
  type: syslog
EOF
  log "acquisition added (/var/log/auth.log)"
fi

# 5. Sizing drop-in (systemd): MemoryMax/CPUQuota from the fleet profile.
for unit in crowdsec crowdsec-firewall-bouncer; do
  install -d -m 0755 "/etc/systemd/system/${unit}.service.d"
  cat > "/etc/systemd/system/${unit}.service.d/sdkops.conf" <<EOF
[Service]
MemoryMax=${MEM_LIMIT}
CPUQuota=${CPU_QUOTA}
EOF
done
systemctl daemon-reload

# 6. Mode wiring.
KEY_FILE="$DIR/bouncer.key"
if [ "$CLIENT" = "1" ]; then
  # Client: the agent authenticates the REMOTE LAPI; the bouncer pulls the
  # decisions from it. The bouncer key is created on the central (the client
  # has no admin rights there) and reaches this node through .env.
  LAPI_URL="${CS_LAPI_URL:-}"
  [ -n "$LAPI_URL" ] || fail "client mode needs CS_LAPI_URL"
  [ -n "${CS_BOUNCER_KEY:-}" ] || fail "client mode needs CS_BOUNCER_KEY (create on the central: cscli bouncers add $BOUNCER)"
  umask 077
  cat > /etc/crowdsec/local_api_credentials.yaml <<EOF
url: ${LAPI_URL}
login: ${CS_LAPI_USER:-}
password: ${CS_LAPI_PASSWORD:-}
EOF
  printf '%s' "$CS_BOUNCER_KEY" > "$KEY_FILE"; chmod 0600 "$KEY_FILE"
  BOUNCER_API_URL="$LAPI_URL"
  BOUNCER_API_KEY="$CS_BOUNCER_KEY"
  log "client mode: agent -> $LAPI_URL, bouncer consumes its decisions"
else
  # Standalone: local LAPI. Wait for it, then (re)register the bouncer.
  if [ -n "$LAPI_LISTEN" ] && [ "$LAPI_LISTEN" != "127.0.0.1:8080" ]; then
    # Serving remote clients from a bare central is opt-in (firewall/peers
    # still gate the port); localhost stays the default.
    sed -i -E "s#(^\s*listen_uri:\s*).*#\1$LAPI_LISTEN#" /etc/crowdsec/config.yaml 2>/dev/null || true
  fi
  systemctl enable crowdsec >/dev/null 2>&1 || true
  systemctl restart crowdsec
  lapi_ok=0
  for i in $(seq 1 30); do
    if cscli lapi status >/dev/null 2>&1; then lapi_ok=1; break; fi
    sleep 2
  done
  [ "$lapi_ok" = 1 ] || fail "local LAPI not answering (journalctl -u crowdsec)"
  BOUNCER_API_URL="http://127.0.0.1:8080"
  BOUNCER_API_KEY="${CS_BOUNCER_KEY:-$(cat "$KEY_FILE" 2>/dev/null || true)}"
  if printf '%s' "$(cscli bouncers list -o json 2>/dev/null || true)" | grep -q "\"$BOUNCER\""; then
    if [ -z "$BOUNCER_API_KEY" ]; then
      cscli bouncers delete "$BOUNCER" >/dev/null 2>&1 || true
      BOUNCER_API_KEY="$(cscli bouncers add "$BOUNCER" -o raw 2>/dev/null | tr -d '\r' | head -1 || true)"
    fi
  else
    BOUNCER_API_KEY="$(cscli bouncers add "$BOUNCER" -o raw 2>/dev/null | tr -d '\r' | head -1 || true)"
  fi
  [ -n "$BOUNCER_API_KEY" ] || fail "could not register bouncer $BOUNCER"
  printf '%s' "$BOUNCER_API_KEY" > "$KEY_FILE"; chmod 0600 "$KEY_FILE"
  log "standalone mode: local LAPI, bouncer $BOUNCER registered"
fi

# 7. Bouncer config: point it at the resolved LAPI and enable nftables.
BFL=/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml
[ -f "$BFL" ] || fail "bouncer config missing ($BFL)"
sed -i -E "s#^api_url:.*#api_url: $BOUNCER_API_URL#" "$BFL"
if grep -qE '^api_key:' "$BFL"; then
  sed -i -E "s#^api_key:.*#api_key: $BOUNCER_API_KEY#" "$BFL"
else
  printf 'api_key: %s\n' "$BOUNCER_API_KEY" >> "$BFL"
fi
grep -qE '^mode:' "$BFL" && sed -i -E "s#^mode:.*#mode: nftables#" "$BFL" || printf 'mode: nftables\n' >> "$BFL"
chmod 0600 "$BFL"

# 8. Start both units and wait for the bouncer to stream decisions.
systemctl enable crowdsec-firewall-bouncer >/dev/null 2>&1 || true
systemctl restart crowdsec-firewall-bouncer
bounced=0
for i in $(seq 1 30); do
  if systemctl is-active --quiet crowdsec-firewall-bouncer; then bounced=1; break; fi
  sleep 2
done
[ "$bounced" = 1 ] || fail "crowdsec-firewall-bouncer not active (journalctl -u crowdsec-firewall-bouncer)"
systemctl is-active --quiet crowdsec || fail "crowdsec not active"

cscli version 2>/dev/null | head -2 || true
cscli bouncers list 2>/dev/null || true
log "node ready ($BOUNCER)"
