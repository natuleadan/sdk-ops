#!/bin/sh
# pgsql-docker init — PostgreSQL 18 + PgDog + SSL + pgbackrest (local or S3)
set -e

PG_USER="${PG_USER:-dev}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"
POOL_SIZE="${PGDOG_POOL_SIZE:-20}"

# S3-compatible storage (optional — overrides default local repo)
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_BUCKET="${S3_BUCKET:-}"
S3_KEY="${S3_KEY:-}"
S3_SECRET="${S3_SECRET:-}"
S3_REGION="${S3_REGION:-auto}"

CONTAINER="${CONTAINER:-pgsql-docker-postgres-1}"

echo "=== pgsql-docker init ==="
echo "User: $PG_USER  DB: $PG_DATABASE  Pool: $POOL_SIZE"

# Create directories
mkdir -p data/pg data/pg-replica data/pgbackrest ssl

# Generate SSL certificates if missing
if [ ! -f ssl/server.key ]; then
  bash gen-certs.sh
  # Fix permissions inside Docker (cross-platform)
  docker run --rm -v "$(pwd)/ssl:/ssl" alpine sh -c '
    chown -R 70:70 /ssl && chmod 600 /ssl/server.key && chmod 644 /ssl/server.crt
  ' 2>/dev/null
fi

# Replace placeholders in config files (cross-platform sed)
_ni() { sed -i.bak "$1" "$2" && rm -f "${2}.bak"; }
for f in pgdog.toml users.toml pgbackrest.conf; do
  [ -f "$f" ] || continue
  _ni "s/PG_USER/$PG_USER/g" "$f"
  _ni "s/PG_PASSWORD/$PG_PASSWORD/g" "$f"
  _ni "s/PG_DATABASE/$PG_DATABASE/g" "$f"
  _ni "s/POOL_SIZE/$POOL_SIZE/g" "$f"
done

# Configure pgbackrest storage backend
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_KEY" ] && [ -n "$S3_SECRET" ]; then
  echo "Backup: S3 ($S3_ENDPOINT/$S3_BUCKET)"
  cat > pgbackrest.conf << EOF
[pgbackrest]
compress-type=zst
compress-level=3
process-max=2
start-fast=y
buffer-path=/tmp

[main]
pg1-path=/var/lib/postgresql/18/docker
pg1-port=5432
pg1-user=$PG_USER

repo1-path=/var/lib/pgbackrest
repo1-retention-full=2
repo1-retention-diff=4
repo1-cipher-type=none
repo1-bundle=y
repo1-block=y

[global]
spool-path=/var/spool/pgbackrest

[global:archive-push]
compress-level=1

[global:archive-get]
compress-level=1

[main:storage]
type=s3
s3-bucket=$S3_BUCKET
s3-region=$S3_REGION
s3-endpoint=$S3_ENDPOINT
s3-key=$S3_KEY
s3-key-secret=$S3_SECRET
EOF
else
  echo "Backup: local (/var/lib/pgbackrest)"
fi



# pgbackrest.conf must be world-readable before the container mounts it
# (postgres runs as UID 70; a 0600 owner-501 file => archive-push EACCES).
chmod 0644 pgbackrest.conf 2>/dev/null || true
chown 70:70 pgbackrest.conf 2>/dev/null || true
chmod 0644 pgdog.toml users.toml postgresql.conf 2>/dev/null || true

# Start primary first (replica starts later after replication user exists).
# --force-recreate: a bind-mounted config change (perms) is NOT picked up by a
# plain `up -d` (docker caches the mount) — recreate so the container reads the
# fixed pgbackrest.conf/postgresql.conf.
docker compose up -d --force-recreate postgres 2>&1 | tail -3

# Wait for PostgreSQL
echo "Waiting for PostgreSQL..."
until docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pg_isready -U "$PG_USER" -h localhost 2>/dev/null; do
  sleep 2
done
echo "  PostgreSQL ready"

# Create the app database if the configured DB is not the default postgres
# (the dev user needs its own DB; idempotent).
if [ "$PG_DATABASE" != "postgres" ]; then
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d postgres -h localhost -tAc \
    "SELECT 1 FROM pg_database WHERE datname='$PG_DATABASE'" 2>/dev/null | grep -q 1 || {
    docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U postgres -d postgres -h localhost -c \
      "CREATE DATABASE $PG_DATABASE OWNER $PG_USER;" 2>&1 | tail -1
  }
fi

# Create replicator user for streaming replication
echo "Configuring replication..."
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -tAc \
  "SELECT 1 FROM pg_roles WHERE rolname='replicator'" 2>/dev/null | grep -q 1 || {
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -c \
    "CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD '${REPLICATOR_PASSWORD:-replicatorpass}';" 2>&1 | tail -1
}
docker exec "$CONTAINER" sh -c \
  "echo 'host replication replicator 0.0.0.0/0 scram-sha-256' >> /var/lib/postgresql/18/docker/pg_hba.conf"
docker exec "$CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -h localhost -c "SELECT pg_reload_conf();" 2>&1 | tail -1
echo "  Replication user: replicator"

# Create pgbackrest stanza (idempotent — already exists / no backup yet is OK;
# never fail the init over the stanza: replicas must still come up).
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pgbackrest --stanza=main stanza-create >/dev/null 2>&1 || true
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pgbackrest --stanza=main check >/dev/null 2>&1 || true
echo "  pgbackrest stanza: main (ready on first backup)"

# Fix ownership so postgres user can read/write repo for archive_command
docker exec "$CONTAINER" chown -R 70:70 /var/lib/pgbackrest 2>/dev/null || true
# Fix SSL permissions inside container (in case bind mount preserved wrong perms)
docker exec "$CONTAINER" chmod 600 /ssl/server.key 2>/dev/null || true

# Start replicas first (need replicator user + stanza), wait for healthy,
# then pgdog (its depends_on: service_healthy would leave it Created if a
# replica is slow — compose abandons the wait).
echo "Starting replicas..."
docker compose up -d pg-replica pg-replica-2 2>&1 | tail -3
echo "  Waiting for replicas..."
for REP in pg-replica pg-replica-2; do
  until docker compose exec -e PGPASSWORD="$PG_PASSWORD" "$REP" pg_isready -U "$PG_USER" -h localhost 2>/dev/null; do
    sleep 5
  done
  echo "  $REP ready"
done
echo "Starting PgDog..."
docker compose up -d pgdog 2>&1 | tail -2
sleep 3

echo "✓ pgsql-docker ready"
echo "  PG:      postgresql://$PG_USER:$PG_PASSWORD@localhost:5432/$PG_DATABASE?sslmode=require"
echo "  PgDog:   postgresql://$PG_USER:$PG_PASSWORD@localhost:6432/$PG_DATABASE?sslmode=require"
echo "  Backup:  bash backup.sh"
echo "  Restore: bash restore.sh --help"
