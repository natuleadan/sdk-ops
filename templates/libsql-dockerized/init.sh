#!/bin/sh
# libsql-dockerized init — TLS, start services, create schema
# All curl commands inside Docker via docker exec
set -e

PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"
REPLICA_CONTAINER="libsql-dockerized-sqld-replica-1"

SQL()     { docker exec "$PRIMARY_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
HC_PRIM() { docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_REP()  { docker exec "$REPLICA_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }

echo "=== libsql-dockerized init ==="

mkdir -p backups tls

if [ ! -f tls/server_key.pem ]; then
  bash gen-certs.sh
fi

echo "Starting sqld..."
docker compose up -d 2>&1 | tail -1

echo -n "Waiting for primary..."
until HC_PRIM; do sleep 2; done
echo " OK"

echo -n "Waiting for replica..."
until HC_REP; do sleep 2; done
echo " OK"

echo "Creating schema..."
SQL '{"statements": [
  "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, content TEXT, embedding F32_BLOB(3))",
  "CREATE INDEX IF NOT EXISTS idx_items_content ON items(content)",
  "INSERT OR IGNORE INTO items VALUES (1, '\''hello'\'', vector('\''[0.1, 0.2, 0.3]'\''))",
  "INSERT OR IGNORE INTO items VALUES (2, '\''world'\'', vector('\''[0.4, 0.5, 0.6]'\''))"
]}' || echo "  Schema may already exist"

echo ""
echo "✓ libsql-dockerized ready"
echo "  curl -d '{\"statements\":[\"SELECT * FROM items\"]}' http://localhost:8080"
echo "  curl -d '{\"statements\":[\"SELECT * FROM items\"]}' http://localhost:8443 (TLS)"
echo "  Backup: bash backup.sh"
echo "  Restore: bash restore.sh --help"
