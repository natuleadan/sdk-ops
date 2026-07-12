#!/bin/sh
# pg-full-bm PostgreSQL entrypoint — installs pgbackrest for WAL archiving
set -e

if ! command -v pgbackrest >/dev/null 2>&1; then
  apk add --no-cache pgbackrest 2>&1 | tail -1
fi

# Ensure pgbackrest dirs are writable by postgres user (PID 70)
mkdir -p /tmp/pgbackrest && chmod 777 /tmp/pgbackrest
chown -R 70:70 /var/lib/pgbackrest 2>/dev/null || true

exec docker-entrypoint.sh "$@"
