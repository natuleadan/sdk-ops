#!/bin/sh
# libsql-full-bm snapshot — callback invoked by sqld --snapshot-exec
set -e

SNAPSHOT_FILE="$1"
NAMESPACE="$2"

echo "[snapshot] $SNAPSHOT_FILE" >> /proc/1/fd/1 2>/dev/null || true

if [ -n "$SNAPSHOT_FILE" ] && [ -f "$SNAPSHOT_FILE" ]; then
  cp "$SNAPSHOT_FILE" "/backups/$(basename "$SNAPSHOT_FILE")" 2>/dev/null || true
  rm -f "$SNAPSHOT_FILE"
fi
