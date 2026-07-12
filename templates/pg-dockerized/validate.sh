#!/bin/sh
# pg-dockerized validate — all checks inside Docker
set -e

CONTAINER="${CONTAINER:-pg-dockerized-postgres-1}"
PG_USER="${PG_USER:-dev}"
PG_PASSWORD="${PG_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"

PSQL_P() { docker exec -e PGPASSWORD="$PG_PASSWORD" pg-dockerized-postgres-1 psql -U "$PG_USER" -h localhost -d "$PG_DATABASE" "$@"; }
PSQL_D() { docker exec -e PGPASSWORD="$PG_PASSWORD" pg-dockerized-postgres-1 psql -U "$PG_USER" -h pgdog -p 6432 -d "$PG_DATABASE" "$@"; }

echo "=== pg-dockerized validate ==="

echo -n "Primary: "
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" pg_isready -U "$PG_USER" -h localhost -q && echo "OK" || echo "FAIL"

echo -n "Replica-1: "
docker exec -e PGPASSWORD="$PG_PASSWORD" pg-dockerized-pg-replica-1 pg_isready -U "$PG_USER" -h localhost -q && echo "OK" || echo "FAIL"

echo -n "Replica-2: "
docker exec -e PGPASSWORD="$PG_PASSWORD" pg-dockerized-pg-replica-2 pg_isready -U "$PG_USER" -h localhost -q && echo "OK" || echo "FAIL"

echo -n "PgDog: "
docker exec -e PGPASSWORD="$PG_PASSWORD" pg-dockerized-postgres-1 psql -U "$PG_USER" -h pgdog -p 6432 -d "$PG_DATABASE" -tAc "SELECT 1" 2>/dev/null | grep -q "1" && echo "OK" || echo "FAIL"

echo -n "SQL via PgDog: "
PSQL_D -c "SELECT 1 AS ok" -qAt 2>/dev/null | grep -q "1" && echo "OK" || echo "FAIL"

echo -n "Replication: "
REPL_STATE=$(PSQL_P -c "SELECT state FROM pg_stat_replication LIMIT 1" -qAt 2>/dev/null)
echo "${REPL_STATE:-no replica connected}"

echo -n "Replica lag: "
LAG=$(docker exec -e PGPASSWORD="$PG_PASSWORD" pg-dockerized-pg-replica-1 psql -U "$PG_USER" -h localhost -d "$PG_DATABASE" -tAc \
  "SELECT EXTRACT(EPOCH FROM now() - pg_last_xact_replay_timestamp())::int" 2>/dev/null)
echo "${LAG:-0}s" || echo "0s"

echo -n "pgbackrest stanza: "
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
  pgbackrest --stanza=main --no-archive-check check 2>/dev/null && echo "OK" || echo "MISSING"

echo -n "pgbackrest latest backup: "
docker exec -e PGPASSWORD="$PG_PASSWORD" "$CONTAINER" \
  pgbackrest --stanza=main info 2>/dev/null | grep -o "backup: [0-9T:-]*" | tail -1 || echo "none"

echo "✓ pg-dockerized ready"
