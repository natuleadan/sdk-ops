#!/bin/sh
# yuga-docker init — bring up the 3-node YugabyteDB cluster and
# provision the app database/user. Idempotent: re-running on a healthy cluster
# is a no-op.
set -e

YB_DB="${YB_DB:-yugabyte}"
YB_USER="${YB_USER:-dev}"
YB_PASSWORD="${YB_PASSWORD:-devpass}"
YB_YSQL_PORT="${YB_YSQL_PORT:-5433}"
YB_MASTER_UI="${YB_MASTER_UI:-7000}"

# Env-driven namespace prefix so a provisioned (fleet) instance names its
# containers uniquely (defaults to the compose project for local runs).
NS="${PROVISION_NS:-yuga-docker}"

echo "=== yuga-docker init ==="
echo "DB: $YB_DB  User: $YB_USER  YSQL port: $YB_YSQL_PORT"

# Start the stack (3 yugabyted nodes). Idempotent — already-running is fine.
# `docker compose ps` succeeds even with zero containers (project exists), so
# check for a live container: a fresh/cleaned dir must run `up -d`.
if docker compose ps -q 2>/dev/null | grep -q .; then
  echo "  → docker compose already initialized"
else
  echo "  → docker compose up (fresh)"
  docker compose up -d
fi

# Wait for the cluster quorum (2/3 masters) and YSQL to accept connections.
# Connect to the `postgres` system DB (always present) — `yugabyte` may have
# been dropped by an old restore.sh (issues #5651/#4938) and must be recreated.
echo "  → waiting for YugabyteDB quorum + YSQL..."
for i in $(seq 1 40); do
  if docker compose exec -T yugabyte-0 bash -lc \
    "bin/ysqlsh -h yugabyte-0 -p 5433 -U yugabyte -d postgres -c 'SELECT 1'" >/dev/null 2>&1; then
    break
  fi
  sleep 5
done

# Create the app role + database (idempotent) via the postgres system DB.
docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U yugabyte -d postgres -v ON_ERROR_STOP=1 <<SQL
SELECT 'CREATE ROLE $YB_USER LOGIN PASSWORD ' || quote_literal('$YB_PASSWORD')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$YB_USER')\\\\gexec
SQL
" 2>/dev/null || true

docker compose exec -T yugabyte-0 bash -lc "
  bin/ysqlsh -h yugabyte-0 -p 5433 -U yugabyte -d postgres -v ON_ERROR_STOP=1 <<SQL
SELECT 'CREATE DATABASE $YB_DB OWNER $YB_USER'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$YB_DB')\\\\gexec
SQL
" 2>&1 | grep -v "already exists" || true

echo "  cluster ready"
echo "  YSQL:  localhost:$YB_YSQL_PORT  (user: $YB_USER / db: $YB_DB)"
echo "  UI:    http://localhost:$YB_MASTER_UI"
