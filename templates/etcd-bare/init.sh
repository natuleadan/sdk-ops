#!/bin/bash
# etcd-bare init - install etcd from the official GitHub release and run the
# member natively under systemd (no Docker), following the yuga-bare/nats-bare
# pattern: pinned tarball + SHA256SUMS check, arch detection, system user,
# idempotent re-runs (restart only when the rendered config/unit differs).
# One member per fleet host; the static bootstrap (initial-cluster over
# peer_ip:2380) is rendered into etcd.conf.yml by sdk-ops (etcdRenderData -
# the same topology as templates/etcd, the docker DCS).
#
# Quorum note: a member alone has no quorum - endpoint health needs 2/3
# members up. This init waits for local health only briefly and exits 0 with
# a warning when the peers have not joined yet (first-member bootstrap).
set -e

# Rendered member context (etcdRenderData - same builder as the docker etcd).
ETCD_NAME="{{ .EtcdName }}"
ETCD_IP="{{ .EtcdIP }}"

DIR="/opt/sdk-ops/services/etcd-bare"
DATA_DIR="/var/lib/etcd"
CONF="$DIR/etcd.conf.yml"
BIN=/usr/local/bin/etcd
CTL=/usr/local/bin/etcdctl
ETCD_VERSION="${ETCD_VERSION:-v3.5.15}"
# etcd --version prints "etcd Version: 3.5.15" (tag without the v prefix).
WANT_VER="${ETCD_VERSION#v}"

# Arch: release assets are -linux-amd64 / -linux-arm64. The asset FILE name
# keeps the "v" prefix of the tag (etcd-v3.5.15-linux-amd64.tar.gz), only the
# version comparison strips it.
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH_SUFFIX="amd64" ;;
  aarch64|arm64) ARCH_SUFFIX="arm64" ;;
  *) echo "ERROR: unsupported arch $ARCH"; exit 1 ;;
esac

echo "=== etcd-bare init ==="
echo "etcd: $ETCD_VERSION  member: $ETCD_NAME @ $ETCD_IP  arch: $ARCH_SUFFIX"

# 1. etcd + etcdctl binaries (pinned release, checksum from the official
#    SHA256SUMS asset - that asset covers every tarball of the release; if it
#    cannot be fetched, verification is skipped rather than failing the install).
if [ ! -x "$BIN" ] || ! "$BIN" --version 2>/dev/null | grep -q "$WANT_VER"; then
  TBALL="etcd-${ETCD_VERSION}-linux-${ARCH_SUFFIX}.tar.gz"
  URL="https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/${TBALL}"
  TMP="$(mktemp -d)"
  echo "  -> downloading $URL"
  curl -fsSL -o "$TMP/$TBALL" "$URL"
  if curl -fsSL -o "$TMP/SHA256SUMS" \
       "https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/SHA256SUMS" 2>/dev/null; then
    grep -E "[[:space:]]${TBALL}\$" "$TMP/SHA256SUMS" | (cd "$TMP" && sha256sum -c -)
  else
    # No SHA256SUMS asset for this release - skipping checksum verification.
    echo "  -> no SHA256SUMS asset for $ETCD_VERSION, skipping checksum"
  fi
  tar xzf "$TMP/$TBALL" -C "$TMP"
  install -m 0755 "$TMP/etcd-${ETCD_VERSION}-linux-${ARCH_SUFFIX}/etcd" "$BIN"
  install -m 0755 "$TMP/etcd-${ETCD_VERSION}-linux-${ARCH_SUFFIX}/etcdctl" "$CTL"
  rm -rf "$TMP"
  echo "  -> installed $BIN ($("$BIN" --version 2>&1 | head -1))"
else
  echo "  -> etcd already installed ($("$BIN" --version 2>&1 | head -1))"
fi

# 2. System user + dirs (idempotent). The etcd user runs the member; the data
#    dir lives outside the service dir so a re-provision never wipes it.
mkdir -p "$DATA_DIR" "$DIR"
id etcd >/dev/null 2>&1 || useradd -r -d "$DATA_DIR" -s /usr/sbin/nologin etcd 2>/dev/null || useradd -r -d "$DATA_DIR" -s /bin/false etcd
chown -R etcd:etcd "$DATA_DIR"
# The rendered conf lands 0600 owned by the deploy user — etcd (the unit user)
# must be able to read it.
chown root:etcd "$CONF" 2>/dev/null || true
chmod 0640 "$CONF"

# 3. etcd.env (systemd EnvironmentFile): a stub the first time only - operator
#    edits (extra env, secrets) are preserved on re-runs.
ENV_FILE="$DIR/etcd.env"
if [ ! -f "$ENV_FILE" ]; then
  umask 077
  cat > "$ENV_FILE" <<'EOF'
# Extra environment for the etcd member (systemd EnvironmentFile).
# Secrets go here - never in etcd.conf.yml or the provision YAML.
#ETCD_LOG_LEVEL=info
EOF
  chown root:root "$ENV_FILE"
fi

# 4. systemd unit (rendered etcd.service -> MemoryMax/CPUQuota from the
#    profile). Reinstall + reload only when the rendered unit differs.
UNIT_SRC="$DIR/etcd.service"
UNIT_DST="/etc/systemd/system/etcd.service"
if [ -f "$UNIT_SRC" ] && ! cmp -s "$UNIT_SRC" "$UNIT_DST"; then
  install -m 0644 "$UNIT_SRC" "$UNIT_DST"
  systemctl daemon-reload
  echo "  -> systemd unit installed"
  UNIT_CHANGED=1
else
  UNIT_CHANGED=0
fi

# 5. Start + enable. Restart only when something actually changed (a running
#    member keeps the old config until it is restarted after a conf change -
#    same gotcha as the dockerized compose mount).
[ -f "$CONF" ] || { echo "ERROR: $CONF missing (render + upload first)"; exit 1; }
CONF_HASH_NOW="$(sha256sum "$CONF" | awk '{print $1}')"
CONF_HASH_FILE="$DIR/.etcd.conf.sha256"
NEED_RESTART=0
if [ ! -f "$CONF_HASH_FILE" ] || [ "$(cat "$CONF_HASH_FILE" 2>/dev/null)" != "$CONF_HASH_NOW" ]; then
  NEED_RESTART=1
  echo "$CONF_HASH_NOW" > "$CONF_HASH_FILE"
fi
[ "$UNIT_CHANGED" -eq 1 ] && NEED_RESTART=1

if ! systemctl is-active --quiet etcd; then
  systemctl enable --now etcd
  echo "  -> etcd started + enabled"
elif [ "$NEED_RESTART" -eq 1 ]; then
  systemctl restart etcd
  echo "  -> config/unit changed: etcd restarted"
else
  echo "  -> etcd already running (config unchanged)"
fi

# 6. Health wait. A lone member has no quorum: endpoint health reports
#    unhealthy until 2/3 members are up, so keep the wait short and warn
#    instead of failing (the quorum forms when the peers join).
sleep 1
systemctl is-active --quiet etcd || { echo "ERROR: etcd unit not running (journalctl -u etcd)"; exit 1; }
echo "  -> waiting for local health (quorum 2/3 once all members join)..."
healthy=0
for i in $(seq 1 20); do
  if ETCDCTL_API=3 "$CTL" --endpoints=127.0.0.1:2379 --command-timeout=4s endpoint health 2>/dev/null | grep -q healthy; then
    healthy=1
    break
  fi
  sleep 2
done
if [ "$healthy" -eq 1 ]; then
  echo "  etcd member healthy"
else
  echo "  WARN: member not healthy yet - the 2/3 quorum forms when the peers join"
fi
echo "  etcd-bare member ready ($ETCD_NAME)"
