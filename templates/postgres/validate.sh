#!/bin/bash
# postgres validate — node-side PG stack health (systemd timer, 5 min).
# Also ENSURES the app role + password match the env (idempotent — restores
# leave a stale dev password behind).
set -u

PATRONI_CONT="$(docker ps --format '{{.Names}}' | grep -E 'patroni' | head -1)"
[ -n "$PATRONI_CONT" ] || { echo "FAIL: no patroni container"; exit 1; }

# 0. Ensure the app role + schema (source .env for the password). Restores can
#    leave a stale dev password or a missing app schema — idempotent ensure.
if [ -f .env ]; then
  . ./.env
  docker exec "$PATRONI_CONT" psql -U postgres -d postgres -tA -w -c "DO \$\$ BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='dev') THEN
      CREATE ROLE dev WITH LOGIN PASSWORD '${PG_APP_PASSWORD:-}';
    ELSE
      ALTER ROLE dev WITH PASSWORD '${PG_APP_PASSWORD:-}';
    END IF;
  END \$\$;" >/dev/null 2>&1
  docker exec "$PATRONI_CONT" psql -U postgres -d postgres -c "CREATE TABLE IF NOT EXISTS link (id serial PRIMARY KEY, short_code text UNIQUE, target_url text)" >/dev/null 2>&1
  docker exec "$PATRONI_CONT" psql -U postgres -d postgres -c "GRANT ALL ON SCHEMA public TO dev; GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO dev; ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO dev; GRANT pg_read_all_settings TO dev; GRANT EXECUTE ON FUNCTION pg_catalog.pg_backup_start(text, boolean) TO dev; GRANT EXECUTE ON FUNCTION pg_catalog.pg_backup_stop(boolean) TO dev; GRANT EXECUTE ON FUNCTION pg_catalog.pg_wal_replay_pause() TO dev; GRANT EXECUTE ON FUNCTION pg_catalog.pg_wal_replay_resume() TO dev;" >/dev/null 2>&1
fi

# 1. Patroni REST (local).
docker exec "$PATRONI_CONT" curl -s --max-time 5 http://127.0.0.1:8008/health 2>/dev/null | grep -q '"state"' \
  || { echo "FAIL: patroni REST down"; exit 1; }

# 2. A cluster leader exists (local patronictl or a peer).
L=$(docker exec "$PATRONI_CONT" patronictl -c /etc/patroni/patroni.yml list 2>/dev/null | grep -c 'Leader')
[ "${L:-0}" -ge 1 ] || { echo "FAIL: no leader"; exit 1; }

# 3. The standby is reachable (peer via the private network).
PEER=$(docker exec "$PATRONI_CONT" patronictl -c /etc/patroni/patroni.yml list 2>/dev/null | grep -E 'Standby|Replica' | awk '{print $2}' | head -1)
if [ -n "$PEER" ]; then
  docker exec "$PATRONI_CONT" curl -s --max-time 5 "http://$PEER:8008/health" 2>/dev/null | grep -q '"state"' \
    || { echo "FAIL: replica REST unreachable ($PEER)"; exit 1; }
fi

# 4. Pooler (if present).
PGDOG=$(docker ps --format '{{.Names}}' | grep -E 'pgdog' | head -1)
if [ -n "$PGDOG" ]; then
  docker exec "$PGDOG" psql -h 127.0.0.1 -p 6432 -U dev -d postgres -tA -w -c 'SELECT 1' >/dev/null 2>&1 \
    || { echo "FAIL: pgdog down"; exit 1; }
fi

echo "OK: postgres stack healthy"
