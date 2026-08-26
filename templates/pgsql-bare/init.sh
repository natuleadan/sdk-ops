#!/bin/sh
# pgsql-bare init — install PostgreSQL natively (no Docker) on the VPS and
# start a single instance. Idempotent. One node per host; for HA use the
# pgsql-cluster template (Patroni) or pgsql-docker (compose stack).
set -e

PG_VERSION="${PG_VERSION:-18}"
PG_DATA="${PG_DATA:-/var/lib/postgresql/$PG_VERSION/main}"
PG_USER="${PG_USER:-postgres}"
PG_APP_USER="${PG_APP_USER:-dev}"
PG_APP_PASSWORD="${PG_APP_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"
LISTEN_ADDR="${PG_LISTEN_ADDR:-0.0.0.0}"
LISTEN_PORT="${PG_LISTEN_PORT:-5432}"

echo "=== pgsql-bare init ==="
echo "PostgreSQL $PG_VERSION  data: $PG_DATA  port: $LISTEN_PORT"

# 1. Install the postgres server (native, no Docker).
if ! command -v psql >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq postgresql-$PG_VERSION postgresql-client-$PG_VERSION >/dev/null 2>&1
fi

# 2. Configure listen address + the app role/db.
CONF="$(pg_lsclusters -h 2>/dev/null | awk '$1=="'$PG_VERSION'" {print $6}' || echo /etc/postgresql/$PG_VERSION/main)"
# Find the config dir for the cluster (Debian/Ubuntu layout).
[ -n "$CONF" ] || CONF="/etc/postgresql/$PG_VERSION/main"

sed -i "s/^#*listen_addresses.*/listen_addresses = '$LISTEN_ADDR'/" "$CONF/postgresql.conf" 2>/dev/null || true
echo "port = $LISTEN_PORT" >> "$CONF/postgresql.conf" 2>/dev/null || true

# 3. Start the cluster.
pg_ctlcluster $PG_VERSION main start 2>/dev/null || service postgresql start 2>/dev/null || true

# 4. Create the app role + database (idempotent) via a temp SQL file (avoids
#    fragile quoting inside `su -c`).
TMP_SQL="$(mktemp)"
cat > "$TMP_SQL" <<EOF
SELECT 'CREATE ROLE $PG_APP_USER LOGIN PASSWORD ' || quote_literal('$PG_APP_PASSWORD')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$PG_APP_USER')\gexec
SELECT 'CREATE DATABASE $PG_DATABASE OWNER $PG_APP_USER'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$PG_DATABASE')\gexec
GRANT ALL PRIVILEGES ON DATABASE $PG_DATABASE TO $PG_APP_USER;
EOF
chown postgres:postgres "$TMP_SQL"
su -s /bin/sh postgres -c "psql -v ON_ERROR_STOP=0 -d postgres -f $TMP_SQL" >/dev/null 2>&1 || true
# Grant schema-public access so the app role can DDL.
su -s /bin/sh postgres -c "psql -d $PG_DATABASE -c 'GRANT ALL ON SCHEMA public TO $PG_APP_USER'" >/dev/null 2>&1 || true
rm -f "$TMP_SQL"

echo "  PostgreSQL ready: postgresql://$PG_APP_USER@$LISTEN_ADDR:$LISTEN_PORT/$PG_DATABASE"
