#!/bin/bash
# Patroni v4+ post_bootstrap — creates the application roles (v4 removed the
# bootstrap.users section). Runs once on the original bootstrap; the replicas
# clone the roles via the basebackup. Local socket trust during the bootstrap.
set -eu

psql -U postgres -v ON_ERROR_STOP=1 <<'SQL'
CREATE ROLE admin WITH LOGIN CREATEROLE CREATEDB PASSWORD '{{ .PGSuperUser }}';
CREATE ROLE dev WITH LOGIN PASSWORD '{{ .PGAppPassword }}';
GRANT CREATE, USAGE ON SCHEMA public TO dev;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO dev;
-- pgbackrest runs as pg1-user=dev (local socket, trust) — it needs to read
-- pg_settings and run the exclusive backup functions.
GRANT pg_read_all_settings TO dev;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_backup_start(text, boolean) TO dev;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_backup_stop(boolean) TO dev;
SQL

# The S3 repo's stanza: a fresh cluster has a NEW system-id — create it (the
# empty repo) or upgrade it (an existing repo with the old system-id).
if ! pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main info >/dev/null 2>&1; then
  pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main stanza-create 2>/dev/null \
    || pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main stanza-upgrade 2>/dev/null \
    || true
else
  pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main stanza-upgrade 2>/dev/null \
    || pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main stanza-create 2>/dev/null \
    || true
fi

echo "post_bootstrap: admin + dev roles created, stanza synced"
