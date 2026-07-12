#!/bin/sh
# pg-dockerized PostgreSQL entrypoint — pgbackrest pre-installed in image
set -e

# Ensure /tmp/pgbackrest is writable by postgres user
mkdir -p /tmp/pgbackrest && chmod 777 /tmp/pgbackrest

exec docker-entrypoint.sh "$@"
