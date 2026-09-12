#!/bin/sh
# df-bare backup - trigger BGSAVE and copy the latest snapshot set to a local
# dir (./backups). The S3 upload lives in backup-s3.sh. Run as root.
set -e

DF_PASSWORD="${DF_PASSWORD:-}"
if [ -z "$DF_PASSWORD" ] && [ -r /etc/dragonfly/primary.conf ]; then
  DF_PASSWORD="$(sed -n 's/^--requirepass=//p' /etc/dragonfly/primary.conf | head -1)"
fi
DF_PASSWORD="${DF_PASSWORD:-dragonfly}"
PRIMARY_DATA="${PRIMARY_DATA:-/var/lib/dragonfly/primary}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATE=$(date +%F-%H%M%S)

RC()     { redis-cli -h 127.0.0.1 -p 6379 -a "$DF_PASSWORD" "$@" 2>/dev/null; }
RC_REP() { redis-cli -h 127.0.0.1 -p 6380 -a "$DF_PASSWORD" "$@" 2>/dev/null; }

echo "=== df-bare backup ==="

if ! systemctl is-active --quiet dragonfly-primary; then
  echo "ERROR: dragonfly-primary is not running"
  exit 1
fi

echo "Triggering BGSAVE..."
RC BGSAVE >/dev/null
echo -n "Waiting for snapshot..."
for i in $(seq 1 30); do
  INPROGRESS=$(RC INFO PERSISTENCE | grep "rdb_bgsave_in_progress" | cut -d: -f2 | tr -d '\r\n')
  [ "$INPROGRESS" = "0" ] || [ -z "$INPROGRESS" ] && break
  sleep 1
done
echo " done"

# Dragonfly persists the snapshot set as dump-<ts>-*.dfs in the data dir.
TIMESTAMP=$(ls "$PRIMARY_DATA"/dump-*summary.dfs 2>/dev/null | sed "s/.*dump-//;s/-summary.*//" | sort | tail -1)
if [ -n "$TIMESTAMP" ]; then
  mkdir -p "$BACKUP_DIR/$DATE"
  cp "$PRIMARY_DATA"/dump-"$TIMESTAMP"-*.dfs "$BACKUP_DIR/$DATE/" 2>/dev/null
  LOCAL_FILE="$BACKUP_DIR/$DATE"
  echo "  Local: $LOCAL_FILE ($(du -sh "$LOCAL_FILE" | cut -f1))"
else
  echo "  WARN: no snapshot files found in $PRIMARY_DATA (BGSAVE failed?)"
fi

# S3 upload lives in backup-s3.sh. This script is local-only:
# BGSAVE -> ./backups.

# Replica BGSAVE
RC_REP BGSAVE >/dev/null 2>&1 || true
echo "  Replica BGSAVE triggered"

echo ""
echo "=== backup complete ==="
if [ -n "$LOCAL_FILE" ]; then
  echo "  Restore: bash restore.sh $LOCAL_FILE"
fi
