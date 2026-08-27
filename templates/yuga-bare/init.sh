#!/bin/sh
# yuga-bare init — install YugabyteDB from the official tarball and start a
# yugabyted node directly on the VPS (no Docker). One node per fleet host; the
# first node is the seed, later nodes join via --join=<seed-ip>. Idempotent.
set -e

YB_RELEASE="${YB_RELEASE:-2026.1.1.1}"    # directory under /releases (no build)
YB_VERSION="${YB_VERSION:-2026.1.1.1-b2}" # the full tarball version (with build)
# Arch: x86_64 (default) or aarch64/arm64 — the tarball suffix differs
# (linux-x86_64 vs el8-aarch64). Detect from the host.
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)   YB_TARBALL_ARCH="linux-x86_64" ;;
  aarch64|arm64)  YB_TARBALL_ARCH="el8-aarch64" ;;
  *) echo "ERROR: unsupported arch $ARCH"; exit 1 ;;
esac
YB_URL="https://software.yugabyte.com/releases/${YB_RELEASE}/yugabyte-${YB_VERSION}-${YB_TARBALL_ARCH}.tar.gz"
YB_SHA_URL="${YB_URL}.sha"
INSTALL_DIR="${YB_INSTALL_DIR:-/opt/yugabyte}"
# The tarball unpacks into yugabyte-<RELEASE> (the directory uses the release
# version without the build suffix).
APP_DIR="$INSTALL_DIR/yugabyte-$YB_RELEASE"
DATA_DIR="${YB_DATA_DIR:-/var/lib/yugabyte}"
JOIN="${YB_JOIN:-}"          # seed node IP (empty = this is the seed)
CLOUD_LOC="${YB_CLOUD_LOCATION:-cloud.region.zone}"

echo "=== yuga-bare init ==="
echo "Version: $YB_VERSION  install: $INSTALL_DIR  join: ${JOIN:-<seed>}"

# 1. Download + verify the tarball (checksum from the official .sha).
if [ ! -x "$APP_DIR/bin/yugabyted" ]; then
  mkdir -p "$INSTALL_DIR"
  cd "$INSTALL_DIR"
  echo "  → downloading $YB_URL"
  TBALL="yugabyte-$YB_VERSION-$YB_TARBALL_ARCH.tar.gz"
  if ! curl -fsSL -o "$TBALL" "$YB_URL"; then
    echo "ERROR: download failed (version $YB_VERSION may not exist)"
    exit 1
  fi
  echo "$(curl -Ls "$YB_SHA_URL") *$TBALL" | shasum --check -
  tar xzf "$TBALL"
  rm -f "$TBALL"
  echo "  → running post_install.sh"
  (cd "$APP_DIR" && ./bin/post_install.sh)
else
  echo "  → already installed"
fi

# 2. Create the data dir + a non-root user for yugabyted (best practice).
mkdir -p "$DATA_DIR"
id yugabyte >/dev/null 2>&1 || useradd -r -d "$DATA_DIR" -s /bin/bash yugabyte 2>/dev/null || true
chown -R yugabyte:yugabyte "$DATA_DIR" "$APP_DIR"

# 3. Start yugabyted as the service user. The seed waits for the quorum; the
#    followers join it. Skip when already running (yugabyted daemonizes and the
#    first start returns only after the cluster is up — no-op on re-run).
YUGABYTED="$APP_DIR/bin/yugabyted"
if su -s /bin/sh yugabyte -c "$YUGABYTED status --base_dir=$DATA_DIR" 2>&1 | grep -q "Running"; then
  echo "  → yugabyted already running"
else
  JOIN_ARGS=""
  [ -n "$JOIN" ] && JOIN_ARGS="--join=$JOIN"
  # yugabyted start stays in the foreground even with --daemon=false on some
  # versions — run it in the background (nohup) so the init completes, then
  # poll for YSQL readiness below.
  su -s /bin/sh yugabyte -c "
    nohup $YUGABYTED start \
      --base_dir=$DATA_DIR \
      --advertise_address=${YB_ADVERTISE:-127.0.0.1} \
      --cloud_location=$CLOUD_LOC \
      --insecure \
      $JOIN_ARGS \
      --tserver_flags=ysql_num_shards_per_tserver=${YB_SHARDS:-4} \
      > $DATA_DIR/logs/yugabyted-init.log 2>&1 &
  " || true
fi

# Wait for YSQL to accept connections (the yugabyted start was backgrounded).
echo "  → waiting for YSQL..."
YSQLSH="$APP_DIR/bin/ysqlsh"
for i in $(seq 1 60); do
  if su -s /bin/sh yugabyte -c "$YSQLSH -h ${YB_ADVERTISE:-127.0.0.1} -p 5433 -U yugabyte -d postgres -c 'SELECT 1'" >/dev/null 2>&1; then
    echo "  YSQL ready"
    break
  fi
  sleep 5
done
touch "$DATA_DIR/.init-done"

# 4. Create the app role + database (idempotent) via ysqlsh. Use a temp SQL
#    file to avoid fragile quoting inside `su -c`.
YB_DB="${YB_DB:-yugabyte}"
YB_APP_USER="${YB_APP_USER:-dev}"
YB_APP_PASSWORD="${YB_APP_PASSWORD:-devpass}"
YSQLSH="$APP_DIR/bin/ysqlsh"
ADDR="${YB_ADVERTISE:-127.0.0.1}"
TMP_SQL="$(mktemp)"
cat > "$TMP_SQL" <<EOF
SELECT 'CREATE ROLE $YB_APP_USER LOGIN PASSWORD ' || quote_literal('$YB_APP_PASSWORD')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$YB_APP_USER')\gexec
SELECT 'CREATE DATABASE $YB_DB OWNER $YB_APP_USER'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$YB_DB')\gexec
GRANT ALL PRIVILEGES ON DATABASE $YB_DB TO $YB_APP_USER;
EOF
chown yugabyte:yugabyte "$TMP_SQL"
su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U yugabyte -d template1 -f $TMP_SQL" >/dev/null 2>&1 || true
# Grant schema-public access on the app database so the app role can DDL.
su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U yugabyte -d $YB_DB -c 'GRANT ALL ON SCHEMA public TO $YB_APP_USER'" >/dev/null 2>&1 || true
rm -f "$TMP_SQL"

echo "  yugabyted node ready (${JOIN:-seed})"
