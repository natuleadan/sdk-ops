#!/bin/bash
# nats-bare init - install nats-server from the official GitHub release and run
# it natively under systemd (no Docker), following the yuga-bare pattern:
# pinned tarball, arch detection, system user, idempotent re-runs. One node per
# fleet host; the mesh (routes/advertise) comes from the rendered nats.conf.
#
# The provision renders this script per node: {{ .ServerName }} / {{ .Advertise }}
# are baked in at render time. Secrets (bcrypt hashes, JetStream at-rest key)
# are rendered into nats.conf from the operator env - nothing here needs one.
set -e

# Rendered node context (same builder as nats-dockerized: natsRenderData).
SERVER_NAME="{{ .ServerName }}"
ADVERTISE="{{ .Advertise }}"

DIR="/opt/sdk-ops/services/nats-bare"
DATA_DIR="/var/lib/nats"
NATS_SERVER_VERSION="${NATS_SERVER_VERSION:-v2.10.24}"
NATS_CLI_VERSION="${NATS_CLI_VERSION:-0.4.0}"
# Arch: the release assets are -linux-amd64 / -linux-arm64. The asset FILE name
# keeps the "v" prefix of the tag (nats-server-v2.10.24-...), only the release
# TAG path uses the full ${NATS_SERVER_VERSION}.
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)   ARCH_SUFFIX="amd64" ;;
  aarch64|arm64)  ARCH_SUFFIX="arm64" ;;
  *) echo "ERROR: unsupported arch $ARCH"; exit 1 ;;
esac

echo "=== nats-bare init ==="
echo "nats-server: $NATS_SERVER_VERSION  node: $SERVER_NAME  advertise: $ADVERTISE  arch: $ARCH_SUFFIX"

# 1. nats-server binary (pinned release, checksum from the official SHA256SUMS
#    asset; that asset covers every tarball of the release - if it cannot be
#    fetched, the verification is skipped rather than failing the install).
BIN=/usr/local/bin/nats-server
WANT_VER="${NATS_SERVER_VERSION#v}"
if [ ! -x "$BIN" ] || ! "$BIN" --version 2>/dev/null | grep -q "$WANT_VER"; then
  TBALL="nats-server-${NATS_SERVER_VERSION}-linux-${ARCH_SUFFIX}.tar.gz"
  URL="https://github.com/nats-io/nats-server/releases/download/${NATS_SERVER_VERSION}/${TBALL}"
  TMP="$(mktemp -d)"
  echo "  -> downloading $URL"
  curl -fsSL -o "$TMP/$TBALL" "$URL"
  if curl -fsSL -o "$TMP/SHA256SUMS" \
       "https://github.com/nats-io/nats-server/releases/download/${NATS_SERVER_VERSION}/SHA256SUMS" 2>/dev/null; then
    grep -E "[[:space:]]${TBALL}\$" "$TMP/SHA256SUMS" | (cd "$TMP" && sha256sum -c -)
  else
    # No SHA256SUMS asset for this release - skipping checksum verification.
    echo "  -> no SHA256SUMS asset for $NATS_SERVER_VERSION, skipping checksum"
  fi
  tar xzf "$TMP/$TBALL" -C "$TMP"
  install -m 0755 "$TMP/nats-server-${NATS_SERVER_VERSION}-linux-${ARCH_SUFFIX}/nats-server" "$BIN"
  rm -rf "$TMP"
  echo "  -> installed $BIN ($("$BIN" --version 2>&1 | head -1))"
else
  echo "  -> nats-server already installed ($("$BIN" --version 2>&1 | head -1))"
fi

# 2. NATS CLI (pinned, same source as the nats-dockerized wiring) into $DIR/nats,
#    plus /usr/local/bin/nats so the scripts can call it from PATH-style paths.
if [ ! -x "$DIR/nats" ]; then
  ZT="$(mktemp -d)"
  curl -fsSL -o "$ZT/nats.zip" \
    "https://github.com/nats-io/natscli/releases/download/v${NATS_CLI_VERSION}/nats-${NATS_CLI_VERSION}-linux-${ARCH_SUFFIX}.zip"
  python3 -m zipfile -e "$ZT/nats.zip" "$ZT/"
  mkdir -p "$DIR"
  install -m 0755 "$ZT/nats-${NATS_CLI_VERSION}-linux-${ARCH_SUFFIX}/nats" "$DIR/nats"
  rm -rf "$ZT"
  echo "  -> installed NATS CLI v$NATS_CLI_VERSION at $DIR/nats"
fi
# Always ensure the symlink exists (idempotent).
ln -sf "$DIR/nats" /usr/local/bin/nats

# 3. System user + dirs (idempotent). The nats user runs the server; the
#    JetStream store lives outside the service dir so a re-provision never
#    wipes the streams.
mkdir -p "$DATA_DIR" "$DIR"
id nats >/dev/null 2>&1 || useradd -r -d "$DATA_DIR" -s /usr/sbin/nologin nats 2>/dev/null || useradd -r -d "$DATA_DIR" -s /bin/false nats
chown -R nats:nats "$DATA_DIR" "$DIR"

# 4. TLS material: self-serve CA + server cert (SANs: node name, advertise IP,
#    127.0.0.1) + client certs, flattened to the layout nats.conf and the
#    backup/validate scripts expect ($DIR/certs/server.pem, app-cert.pem, ...).
if [ ! -f "$DIR/certs/server.pem" ]; then
  SANS="DNS:$SERVER_NAME,IP:127.0.0.1"
  case "$ADVERTISE" in
    ""|127.0.0.1) ;;
    *) SANS="DNS:$SERVER_NAME,IP:$ADVERTISE,IP:127.0.0.1" ;;
  esac
  GEN_NODES="$SERVER_NAME|$SANS" GEN_OUT="$DIR/certs" bash "$DIR/gen-certs.sh"
  cp -f "$DIR/certs/server/$SERVER_NAME.pem" "$DIR/certs/server.pem"
  cp -f "$DIR/certs/server/$SERVER_NAME.key" "$DIR/certs/server.key"
  for u in app sys svc; do
    cp -f "$DIR/certs/client/$u-cert.pem" "$DIR/certs/$u-cert.pem"
    cp -f "$DIR/certs/client/$u-key.pem" "$DIR/certs/$u-key.pem"
  done
  find "$DIR/certs" -type f -name "*.key" -exec chmod 0600 {} +
  chown -R nats:nats "$DIR/certs"
else
  echo "  -> certs present ($DIR/certs)"
fi

# 5. systemd unit (rendered nats-server.service -> MemoryMax/CPUQuota from the
#    profile). Reinstall + reload only when the rendered unit differs.
UNIT_SRC="$DIR/nats-server.service"
UNIT_DST="/etc/systemd/system/nats-server.service"
if [ -f "$UNIT_SRC" ] && ! cmp -s "$UNIT_SRC" "$UNIT_DST"; then
  install -m 0644 "$UNIT_SRC" "$UNIT_DST"
  systemctl daemon-reload
  echo "  -> systemd unit installed"
  UNIT_CHANGED=1
else
  UNIT_CHANGED=0
fi

# 6. Start + enable. Restart only when something actually changed (the config
#    hash mirrors the dockerized gotcha: a container/runtime keeps running the
#    old config until it is restarted after a nats.conf change).
if [ ! -f "$DIR/nats.conf" ]; then
  echo "ERROR: $DIR/nats.conf missing (render + upload first)"
  exit 1
fi
CONF_HASH_NOW="$(sha256sum "$DIR/nats.conf" | awk '{print $1}')"
CONF_HASH_FILE="$DIR/.nats.conf.sha256"
NEED_RESTART=0
if [ ! -f "$CONF_HASH_FILE" ] || [ "$(cat "$CONF_HASH_FILE" 2>/dev/null)" != "$CONF_HASH_NOW" ]; then
  NEED_RESTART=1
  echo "$CONF_HASH_NOW" > "$CONF_HASH_FILE"
fi
[ "$UNIT_CHANGED" -eq 1 ] && NEED_RESTART=1

if ! systemctl is-active --quiet nats-server; then
  systemctl enable --now nats-server
  echo "  -> nats-server started + enabled"
elif [ "$NEED_RESTART" -eq 1 ]; then
  systemctl restart nats-server
  echo "  -> config/unit changed: nats-server restarted"
else
  echo "  -> nats-server already running (config unchanged)"
fi

# 7. Health wait: the monitoring endpoint answers on loopback (conf `http:`).
echo "  -> waiting for the monitoring endpoint..."
healthy=0
for i in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:8222/ >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 2
done
if [ "$healthy" -ne 1 ]; then
  echo "ERROR: nats-server not healthy after 60s (journalctl -u nats-server)"
  exit 1
fi
echo "  nats-server healthy"
echo "  nats-bare node ready ($SERVER_NAME)"
