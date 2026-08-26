#!/bin/bash
# postgres restore — pgbackrest restore on the POSTGRES HOST (local config).
# pgbackrest 2.59 requires the restore to run on the postgres host (not the
# backup-server). The DR flow (provision restore:true) wipes the data dirs,
# restores from the LATEST FULL backup (a diff/incr can be tiny/empty — the
# full is always the complete base), then starts the restored node FIRST.
# Usage: restore.sh [--type=immediate|--type=time --target="<ISO>"]
set -euo pipefail

TYPE="${1:---type=immediate}"
TARGET=""
case "${2:-}" in
  --target=*) TARGET="${2}" ;;
  --target) TARGET="--target=${3:-}" ;;
esac
STANZA="${PG_STANZA:-main}"
CONF="/etc/pgbackrest/pgbackrest.local.conf"

if [ ! -f "$CONF" ]; then
  echo "ERROR: $CONF not found"
  exit 1
fi

# The DR restores from the LATEST FULL backup (the complete base — never a
# diff/incr: a tiny diff produces an empty-looking restore).
LATEST_FULL=$(sudo -u pgxbs pgbackrest --config="$CONF" --stanza="$STANZA" info 2>/dev/null \
  | grep -oE 'full backup: [0-9]{8}-[0-9]{6}F' | tail -1 | cut -d' ' -f3)
if [ -n "$LATEST_FULL" ]; then
  SET="--set=$LATEST_FULL"
  echo "Restoring from the latest FULL: $LATEST_FULL"
else
  SET=""
  echo "WARNING: no full backup found — restoring the latest backup"
fi

# The restore must run with the postgres STOPPED — a stale postmaster.pid
# aborts the restore. The data dir may hold a leftover pid from a previous
# run (the wipe happened, but a crashed postgres left it).
sudo rm -f /opt/sdk-ops/services/postgres/data/patroni/data/postmaster.pid

# The explicit --pg1-path (the REAL dir, not the /var/lib symlink) — the
# restore's chown would fail on the symlink ("Operation not permitted").
sudo -u pgxbs pgbackrest --config="$CONF" --stanza="$STANZA" restore \
  --pg1-path=/opt/sdk-ops/services/postgres/data/patroni/data \
  --delta $SET "$TYPE" $TARGET 2>&1 | tail -2
sudo chown -R 70:70 /opt/sdk-ops/services/postgres/data/patroni/data
sudo chmod 0700 /opt/sdk-ops/services/postgres/data/patroni/data
echo "OK: restore ($TYPE $TARGET)"
