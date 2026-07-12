#!/bin/sh
# pg-full-bm init — PostgreSQL 18 + PgDog + SSL + pgbackrest (local or S3)
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

CONTAINER="${CONTAINER:-pg-full-bm-postgres-1}"

echo "=== pg-full-bm init ==="
echo "User: $PG_USER  DB: $PG_DATABASE  Pool: $POOL_SIZE"

# Create directories
mkdir -p data/pg data/pg-replica data/pgbackrest ssl

# Generate self-signed SSL certificates BEFORE starting PostgreSQL
if [ ! -f ssl/server.key ]; then
  echo "Generating SSL certificates..."
  openssl req -x509 -newkey rsa:4096 -keyout ssl/server.key -out ssl/server.crt \
    -days 365 -nodes -subj "/CN=postgres" 2>/dev/null
  chown 70:70 ssl/server.key ssl/server.crt 2>/dev/null
  chmod 600 ssl/server.key
  cp ssl/server.crt ssl/root.crt
  echo "  SSL certs generated"
fi

# Replace placeholders in config files
for f in pgdog.toml users.toml pgbackrest.conf; do
  if [ -f "$f" ]; then
    sed -i "s/PG_USER/$PG_USER/g" "$f"
    sed -i "s/PG_PASSWORD/$PG_PASSWORD/g" "$f"
    sed -i "s/PG_DATABASE/$PG_DATABASE/g" "$f"
    sed -i "s/POOL_SIZE/$POOL_SIZE/g" "$f"
  fi
done

# Configure pgbackrest storage backend
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_KEY" ] && [ -n "$S3_SECRET" ]; then
  echo "Backup: S3 ($S3_ENDPOINT/$S3_BUCKET)"
  cat > pgbackrest.conf << EOF
[pgbackrest]
compress-type=gz
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



# Start primary first (replica starts later after replication user exists)
docker compose up -d postgres 2>&1 | tail -3

# Wait for PostgreSQL
echo "Waiting for PostgreSQL..."
until docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pg_isready -U "$PG_USER" -h localhost 2>/dev/null; do
  sleep 2
done
echo "  PostgreSQL ready"

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

# Create pgbackrest stanza
echo "Creating pgbackrest stanza..."
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pgbackrest --stanza=main stanza-create 2>&1 | tail -1 || {
  echo "  Retrying..."
  docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pgbackrest --stanza=main stanza-create 2>&1 | tail -1
}

# Fix ownership so postgres user (PID 70) can read/write repo for archive_command
docker exec "$CONTAINER" chown -R 70:70 /var/lib/pgbackrest 2>/dev/null

# Start replica + pgdog (need replicator user + stanza first)
echo "Starting services..."
docker compose up -d pg-replica pgdog 2>&1 | tail -3
echo "  Waiting for replica..."
sleep 10
until docker exec -e PGPASSWORD="$PG_PASSWORD" pg-full-bm-pg-replica-1 pg_isready -U "$PG_USER" -h localhost 2>/dev/null; do
  sleep 5
done
echo "  Replica ready"

echo "✓ pg-full-bm ready"
echo "  PG:      postgresql://$PG_USER:$PG_PASSWORD@localhost:5432/$PG_DATABASE?sslmode=require"
echo "  PgDog:   postgresql://$PG_USER:$PG_PASSWORD@localhost:6432/$PG_DATABASE?sslmode=require"
echo "  Backup:  bash backup.sh"
echo "  Restore: bash restore.sh --help"
