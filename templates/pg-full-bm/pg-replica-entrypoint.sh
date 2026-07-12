#!/bin/sh
# pg-full-bm replica entrypoint — clones primary via pg_basebackup, starts as standby
set -e

if ! command -v pgbackrest >/dev/null 2>&1; then
  apk add --no-cache pgbackrest 2>&1 | tail -1
fi

mkdir -p /tmp/pgbackrest && chmod 777 /tmp/pgbackrest

PGDATA="${PGDATA:-/var/lib/postgresql/18/docker}"

if [ ! -f "$PGDATA/PG_VERSION" ]; then
  rm -rf "$PGDATA"
  echo "Replica: waiting for primary..."
  until pg_isready -h "$PRIMARY_HOST" -p "$PRIMARY_PORT" -U replicator 2>/dev/null; do
    sleep 3
  done
  echo "Replica: cloning from $PRIMARY_HOST:$PRIMARY_PORT..."
  PGPASSWORD="$REPLICATOR_PASSWORD" pg_basebackup -h "$PRIMARY_HOST" -p "$PRIMARY_PORT" \
    -U replicator -D "$PGDATA" -P -v --wal-method=stream --checkpoint=fast 2>&1 | tail -3
  touch "$PGDATA/standby.signal"
  cat > "$PGDATA/postgresql.auto.conf" << EOF
primary_conninfo = 'host=$PRIMARY_HOST port=$PRIMARY_PORT user=replicator password=$REPLICATOR_PASSWORD sslmode=prefer'
EOF
  chown -R 70:70 "$PGDATA"
  chmod 755 "$(dirname "$PGDATA")"  # entrypoint re-execs as postgres, needs parent accessible
  echo "Replica: clone complete, starting standby"
fi

exec docker-entrypoint.sh "$@"
