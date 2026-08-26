#!/bin/bash
# pgsql-bare validate — check the native postgres is up and the app role can
# connect. Exits non-zero on failure.
set -e

PG_APP_USER="${PG_APP_USER:-dev}"
PG_APP_PASSWORD="${PG_APP_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"
LISTEN_PORT="${PG_LISTEN_PORT:-5432}"
PASS=0
FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

echo "=== pgsql-bare validate ==="

# 1. postgres accepting connections.
if su -s /bin/sh postgres -c "pg_isready -h 127.0.0.1 -p $LISTEN_PORT -q"; then
  ok "postgres accepting connections"
else
  bad "postgres not accepting connections"
fi

# 2. App role login + write/read.
if PGPASSWORD="$PG_APP_PASSWORD" psql -h 127.0.0.1 -p "$LISTEN_PORT" -U "$PG_APP_USER" -d "$PG_DATABASE" -v ON_ERROR_STOP=1 -c "
  CREATE TABLE IF NOT EXISTS validate(id INT PRIMARY KEY, v TEXT);
  INSERT INTO validate VALUES (1, 'ok') ON CONFLICT (id) DO UPDATE SET v='ok';
  SELECT v FROM validate WHERE id=1;
" 2>&1 | grep -q ok; then
  ok "app role write/read"
else
  bad "app role write/read"
fi

echo
echo "result: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
