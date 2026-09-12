#!/bin/sh
# df-bare init - install Dragonfly natively (no Docker) and start the cluster:
# primary + 2 replicas as systemd units behind a native HAProxy TLS entrypoint
# on :6443. Idempotent: identical rendered config leaves the running units
# untouched; only what changed gets installed and restarted. Run as root
# (the provisioner runs it as: sudo bash init.sh).
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"

# --- Rendered resource hints (profiles.yaml via kvRenderData) ---------------
DF_MEM="{{ .DF_MEM }}"
DF_CPUS="{{ .DF_CPUS }}"
HAPROXY_MEM="{{ .HAPROXY_MEM }}"
HAPROXY_CPUS="{{ .HAPROXY_CPUS }}"
[ -n "$DF_MEM" ]       || DF_MEM="512m"
[ -n "$DF_CPUS" ]      || DF_CPUS="1"
[ -n "$HAPROXY_MEM" ]  || HAPROXY_MEM="256m"
[ -n "$HAPROXY_CPUS" ] || HAPROXY_CPUS="0.25"

# --- Fixed topology (must match the rendered flagfiles) ---------------------
# primary :6379 (admin 10001) | replica-1 :6380 (admin 10002) |
# replica-2 :6381 (admin 10003), all loopback-only; HAProxy TLS on :6443.
PRIMARY_PORT="6379"
REPLICA1_PORT="6380"
REPLICA2_PORT="6381"
PRIMARY_ADMIN="10001"
REPLICA1_ADMIN="10002"
REPLICA2_ADMIN="10003"
HAPROXY_TLS_PORT="6443"

# --- Operator env (secrets never live in files) ------------------------------
DF_VERSION="${DF_VERSION:-v1.30.1}"
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
case "$DF_PASSWORD" in
  *[[:space:]=]*) echo "ERROR: DF_PASSWORD must not contain spaces or '=' (gflags flagfile)"; exit 1 ;;
esac

echo "=== df-bare init ==="
echo "Version: $DF_VERSION  HAProxy TLS: :$HAPROXY_TLS_PORT"

# 1. System packages (haproxy + redis-cli + s3cmd + curl), only what is missing.
export DEBIAN_FRONTEND=noninteractive
MISSING=""
command -v haproxy   >/dev/null 2>&1 || MISSING="$MISSING haproxy"
command -v redis-cli >/dev/null 2>&1 || MISSING="$MISSING redis-tools"
command -v s3cmd     >/dev/null 2>&1 || MISSING="$MISSING s3cmd"
command -v curl      >/dev/null 2>&1 || MISSING="$MISSING curl"
if [ -n "$MISSING" ]; then
  echo "  -> apt-get install$MISSING"
  apt-get update -qq
  apt-get install -y -qq $MISSING
fi

# 2. Dragonfly binary from the pinned official GitHub release.
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  DF_ARCH="x86_64" ;;
  aarch64|arm64) DF_ARCH="aarch64" ;;
  *) echo "ERROR: unsupported arch $ARCH"; exit 1 ;;
esac
DF_BIN="/usr/local/bin/dragonfly"
DF_VER_FILE="/usr/local/lib/dragonfly-version"
if [ ! -x "$DF_BIN" ] || [ "$(cat "$DF_VER_FILE" 2>/dev/null)" != "$DF_VERSION" ]; then
  echo "  -> downloading dragonfly $DF_VERSION ($DF_ARCH)"
  REL_URL="https://github.com/dragonflydb/dragonfly/releases/download/$DF_VERSION"
  DL="$(mktemp -d)"
  TB="$DL/dragonfly-$DF_ARCH.tar.gz"
  # Official asset naming is dragonfly-<arch>.tar.gz (all releases to date);
  # older tags shipped dragonfly-<ver>-<arch>.tar.gz - fall back once, then
  # fail with a clear message.
  if ! curl -fsSL -o "$TB" "$REL_URL/dragonfly-$DF_ARCH.tar.gz"; then
    if ! curl -fsSL -o "$TB" "$REL_URL/dragonfly-${DF_VERSION#v}-$DF_ARCH.tar.gz"; then
      echo "ERROR: no dragonfly asset for $DF_VERSION ($DF_ARCH) under $REL_URL"
      exit 1
    fi
  fi
  tar xzf "$TB" -C "$DL"
  if [ -f "$DL/dragonfly-$DF_ARCH" ]; then
    mkdir -p /usr/local/bin
    install -m 0755 "$DL/dragonfly-$DF_ARCH" "$DF_BIN"
  elif [ -f "$DL/dragonfly" ]; then
    mkdir -p /usr/local/bin
    install -m 0755 "$DL/dragonfly" "$DF_BIN"
  else
    echo "ERROR: dragonfly binary not found inside the tarball"
    exit 1
  fi
  mkdir -p /usr/local/lib
  echo "$DF_VERSION" > "$DF_VER_FILE"
  rm -rf "$DL"
else
  echo "  -> dragonfly already installed"
fi

# 3. System user + data dirs.
id dragonfly >/dev/null 2>&1 || useradd -r -d /var/lib/dragonfly -s /usr/sbin/nologin dragonfly
mkdir -p /var/lib/dragonfly/primary /var/lib/dragonfly/replica-1 /var/lib/dragonfly/replica-2 /etc/dragonfly
chown -R dragonfly:dragonfly /var/lib/dragonfly

# 4. TLS certificates (CA + server PEM for HAProxy).
if [ ! -f "$DIR/ssl/server.pem" ]; then
  TLS_DIR="$DIR/ssl" bash "$DIR/gen-certs.sh"
else
  echo "  -> certs already present in $DIR/ssl"
fi

# 5. Rendered configs -> system locations (only when the content changes).
TMPD="$(mktemp -d)"
trap 'rm -rf "$TMPD"' EXIT

install_file() { # src dst mode -> rc 0 = installed, 1 = unchanged
  if [ -f "$2" ] && cmp -s "$1" "$2"; then
    return 1
  fi
  mkdir -p "$(dirname "$2")"
  install -m "$3" "$1" "$2"
  echo "  -> installed $2"
  return 0
}

# Docker-style sizes (256m) -> systemd syntax (256M); the CPUQuota placeholder
# is rewritten from the profile cpus (0.5 -> 50%).
normalize_unit() { # src dst cpuquota
  awk -v q="$3" '
    /^MemoryMax=[0-9.]+m$/ { sub(/m$/, "M") }
    /^MemoryMax=[0-9.]+g$/ { sub(/g$/, "G") }
    /^CPUQuota=/           { sub(/=.*/, "=" q "%") }
    { print }
  ' "$1" > "$2"
}

CPUQ="$(awk -v c="$DF_CPUS" 'BEGIN{printf "%.0f", c*100}')"
HACPUQ="$(awk -v c="$HAPROXY_CPUS" 'BEGIN{printf "%.0f", c*100}')"

CONF_CHANGED=0
UNITS_CHANGED=0
HAPROXY_CHANGED=0

if install_file "$DIR/dragonfly-primary.conf" /etc/dragonfly/primary.conf 0600; then CONF_CHANGED=1; fi
if install_file "$DIR/dragonfly-replica.conf" /etc/dragonfly/replica-1.conf 0600; then CONF_CHANGED=1; fi
# replica-2 = replica-1 with its own ports and data dir.
sed -e 's/--port=6380/--port=6381/' \
    -e 's/--admin_port=10002/--admin_port=10003/' \
    -e 's|/var/lib/dragonfly/replica-1|/var/lib/dragonfly/replica-2|' \
    -e 's/--dbfilename=replica-/--dbfilename=replica2-/' \
    "$DIR/dragonfly-replica.conf" > "$TMPD/replica-2.conf"
if install_file "$TMPD/replica-2.conf" /etc/dragonfly/replica-2.conf 0600; then CONF_CHANGED=1; fi
chown dragonfly:dragonfly /etc/dragonfly/primary.conf /etc/dragonfly/replica-1.conf /etc/dragonfly/replica-2.conf 2>/dev/null || true

normalize_unit "$DIR/dragonfly-primary.service"   "$TMPD/dragonfly-primary.service"   "$CPUQ"
normalize_unit "$DIR/dragonfly-replica-1.service" "$TMPD/dragonfly-replica-1.service" "$CPUQ"
normalize_unit "$DIR/dragonfly-replica-2.service" "$TMPD/dragonfly-replica-2.service" "$CPUQ"
if install_file "$TMPD/dragonfly-primary.service"   /etc/systemd/system/dragonfly-primary.service   0644; then UNITS_CHANGED=1; fi
if install_file "$TMPD/dragonfly-replica-1.service" /etc/systemd/system/dragonfly-replica-1.service 0644; then UNITS_CHANGED=1; fi
if install_file "$TMPD/dragonfly-replica-2.service" /etc/systemd/system/dragonfly-replica-2.service 0644; then UNITS_CHANGED=1; fi

normalize_unit "$DIR/haproxy-df-bare.conf" "$TMPD/haproxy-df-bare.conf" "$HACPUQ"
if install_file "$TMPD/haproxy-df-bare.conf" /etc/systemd/system/haproxy.service.d/df-bare.conf 0644; then HAPROXY_CHANGED=1; fi

if install_file "$DIR/haproxy.cfg" /etc/haproxy/hap.cfg 0644; then HAPROXY_CHANGED=1; fi
if ! cmp -s "$DIR/ssl/server.pem" /etc/haproxy/ssl/server.pem; then
  install -d -m 0755 /etc/haproxy/ssl
  install -o haproxy -g haproxy -m 0600 "$DIR/ssl/server.pem" /etc/haproxy/ssl/server.pem
  HAPROXY_CHANGED=1
fi
# The distro haproxy unit reads CONFIG from /etc/default/haproxy.
if grep -q '^CONFIG=' /etc/default/haproxy 2>/dev/null; then
  sed -i 's|^CONFIG=.*|CONFIG="/etc/haproxy/hap.cfg"|' /etc/default/haproxy
else
  echo 'CONFIG="/etc/haproxy/hap.cfg"' >> /etc/default/haproxy
fi

# 6. systemd: start the primary first, then the replicas, then HAProxy.
systemctl daemon-reload
systemctl enable dragonfly-primary dragonfly-replica-1 dragonfly-replica-2 >/dev/null 2>&1 || true
systemctl enable haproxy >/dev/null 2>&1 || true

echo "Starting Dragonfly primary..."
if [ "$UNITS_CHANGED" = 1 ] || [ "$CONF_CHANGED" = 1 ] || ! systemctl is-active --quiet dragonfly-primary; then
  systemctl restart dragonfly-primary || { echo "ERROR: dragonfly-primary failed (journalctl -u dragonfly-primary -n 50)"; exit 1; }
fi

echo -n "Waiting for primary :$PRIMARY_PORT..."
i=0
while [ "$i" -lt 60 ]; do
  if redis-cli -h 127.0.0.1 -p "$PRIMARY_PORT" -a "$DF_PASSWORD" PING 2>/dev/null | grep -q PONG; then
    break
  fi
  i=$((i + 1))
  sleep 2
done
if ! redis-cli -h 127.0.0.1 -p "$PRIMARY_PORT" -a "$DF_PASSWORD" PING 2>/dev/null | grep -q PONG; then
  echo " FAIL"
  echo "ERROR: primary did not come up (journalctl -u dragonfly-primary -n 50)"
  exit 1
fi
echo " OK"

echo "Starting replicas..."
for u in dragonfly-replica-1 dragonfly-replica-2; do
  if [ "$UNITS_CHANGED" = 1 ] || [ "$CONF_CHANGED" = 1 ] || ! systemctl is-active --quiet "$u"; then
    systemctl restart "$u" || { echo "ERROR: $u failed (journalctl -u $u -n 50)"; exit 1; }
  fi
done

echo -n "Waiting for replicas :$REPLICA1_PORT :$REPLICA2_PORT..."
i=0
while [ "$i" -lt 60 ]; do
  R1=0; R2=0
  redis-cli -h 127.0.0.1 -p "$REPLICA1_PORT" -a "$DF_PASSWORD" PING 2>/dev/null | grep -q PONG && R1=1
  redis-cli -h 127.0.0.1 -p "$REPLICA2_PORT" -a "$DF_PASSWORD" PING 2>/dev/null | grep -q PONG && R2=1
  [ "$R1" = 1 ] && [ "$R2" = 1 ] && break
  i=$((i + 1))
  sleep 2
done
if ! redis-cli -h 127.0.0.1 -p "$REPLICA1_PORT" -a "$DF_PASSWORD" PING 2>/dev/null | grep -q PONG; then
  echo " FAIL"
  echo "ERROR: replica-1 did not come up (journalctl -u dragonfly-replica-1 -n 50)"
  exit 1
fi
if ! redis-cli -h 127.0.0.1 -p "$REPLICA2_PORT" -a "$DF_PASSWORD" PING 2>/dev/null | grep -q PONG; then
  echo " FAIL"
  echo "ERROR: replica-2 did not come up (journalctl -u dragonfly-replica-2 -n 50)"
  exit 1
fi
echo " OK"

echo "Configuring replication..."
for port in "$REPLICA1_PORT" "$REPLICA2_PORT"; do
  if redis-cli -h 127.0.0.1 -p "$port" -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep -q "master_link_status:up"; then
    echo "  replica :$port already streaming"
  else
    # In cluster_mode the replication commands go through the admin port.
    if [ "$port" = "$REPLICA2_PORT" ]; then admin="$REPLICA2_ADMIN"; else admin="$REPLICA1_ADMIN"; fi
    redis-cli -h 127.0.0.1 -p "$admin" -a "$DF_PASSWORD" REPLICAOF 127.0.0.1 "$PRIMARY_PORT" >/dev/null 2>&1 || true
    echo "  REPLICAOF 127.0.0.1:$PRIMARY_PORT sent to replica :$port"
  fi
done

echo -n "Waiting for replication..."
i=0
while [ "$i" -lt 45 ]; do
  R1=0; R2=0
  redis-cli -h 127.0.0.1 -p "$REPLICA1_PORT" -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep -q "master_link_status:up" && R1=1
  redis-cli -h 127.0.0.1 -p "$REPLICA2_PORT" -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep -q "master_link_status:up" && R2=1
  [ "$R1" = 1 ] && [ "$R2" = 1 ] && break
  i=$((i + 1))
  sleep 2
done
if ! redis-cli -h 127.0.0.1 -p "$REPLICA1_PORT" -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep -q "master_link_status:up"; then
  echo " FAIL"
  echo "ERROR: replica-1 did not link to the primary"
  exit 1
fi
if ! redis-cli -h 127.0.0.1 -p "$REPLICA2_PORT" -a "$DF_PASSWORD" INFO REPLICATION 2>/dev/null | grep -q "master_link_status:up"; then
  echo " FAIL"
  echo "ERROR: replica-2 did not link to the primary"
  exit 1
fi
echo " OK"

echo "Configuring replication..."
# Legacy replication (no cluster_mode): plain REPLICAOF. The replicas keep
# serving reads locally (cluster_mode would redirect every read to the
# master and kill the HAProxy read/write split).
redis-cli -h 127.0.0.1 -p "$REPLICA1_ADMIN" -a "$DF_PASSWORD" REPLICAOF 127.0.0.1 "$PRIMARY_PORT" >/dev/null 2>&1 || true
redis-cli -h 127.0.0.1 -p "$REPLICA2_ADMIN" -a "$DF_PASSWORD" REPLICAOF 127.0.0.1 "$PRIMARY_PORT" >/dev/null 2>&1 || true
echo "  REPLICAOF configured"

echo "Starting HAProxy..."
if [ "$HAPROXY_CHANGED" = 1 ] || ! systemctl is-active --quiet haproxy; then
  systemctl restart haproxy || { echo "ERROR: haproxy failed (journalctl -u haproxy -n 50)"; exit 1; }
fi

echo ""
echo "[OK] df-bare ready"
echo "  TLS entrypoint : $HAPROXY_TLS_PORT (writes -> primary, reads -> replicas round-robin)"
echo "  Primary        : 127.0.0.1:$PRIMARY_PORT (admin $PRIMARY_ADMIN)"
echo "  Replicas       : 127.0.0.1:$REPLICA1_PORT / 127.0.0.1:$REPLICA2_PORT (admin $REPLICA1_ADMIN/$REPLICA2_ADMIN)"
echo "  Validate: bash validate.sh"
echo "  Backup:   bash backup-s3.sh"
