#!/bin/bash
# postgres backup — pgbackrest backup (host-side, 2.59, the pg wiring writes
# /etc/pgbackrest/pgbackrest.conf). backup_mode=leader (default): only the
# current primary backs up — replicas skip; server: every node backs up.
# Types: full (default) | diff | incr.
set -euo pipefail

TYPE="${1:-full}"
STANZA="${PG_STANZA:-main}"
MODE="{{ .BackupMode }}"
CONF="/etc/pgbackrest/pgbackrest.conf"
[ -f "$CONF" ] || CONF="/etc/pgbackrest/pgbackrest.local.conf"

if [ ! -f "$CONF" ]; then
  echo "ERROR: $CONF not found — the pg wiring did not run"
  exit 1
fi

if [ "$MODE" = "leader" ]; then
  if ! curl -fsS --max-time 5 http://127.0.0.1:8008/primary >/dev/null 2>&1; then
    echo "SKIP: not the primary (backup_mode=leader)"
    exit 0
  fi
fi

sudo -u pgxbs pgbackrest --config="$CONF" --stanza="$STANZA" --type="$TYPE" backup 2>&1 | tail -2 \
  || { echo "ERROR: backup ($TYPE) failed"; exit 1; }
echo "OK: $TYPE backup ($MODE)"
