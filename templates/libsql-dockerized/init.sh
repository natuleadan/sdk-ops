#!/bin/sh
# libsql-dockerized init — TLS, etcd, start services, controller, create schema
# All curl commands inside Docker via docker exec
set -e

PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"
REPLICA_CONTAINER="libsql-dockerized-sqld-replica-1"
REPLICA2_CONTAINER="libsql-dockerized-sqld-replica-2-1"

SQL()      { docker exec "$PRIMARY_CONTAINER" curl -sf --max-time 10 -X POST http://localhost:8080 -H "Content-Type: application/json" -d "$1" 2>/dev/null; }
HC_PRIM()  { docker exec "$PRIMARY_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_REP()   { docker exec "$REPLICA_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_REP2()  { docker exec "$REPLICA2_CONTAINER" curl -sf http://localhost:8080/health > /dev/null 2>&1; }
HC_ETCD()  { docker exec "libsql-dockerized-etcd-1" etcdctl endpoint health > /dev/null 2>&1; }
HC_CTRL()  { curl -sf http://localhost:${CONTROLLER_PORT:-9090}/health > /dev/null 2>&1; }

echo "=== libsql-dockerized init ==="

mkdir -p backups tls

if [ ! -f tls/server_key.pem ]; then
  bash gen-certs.sh
fi

echo "Starting etcd..."
docker compose up -d etcd 2>&1 | tail -1

echo -n "Waiting for etcd..."
until HC_ETCD; do sleep 2; done
echo " OK"

# --- DR: auto-restore from S3 if data dir is empty ---
if [ -n "${S3_ACCESS_KEY:-}" ] && [ -n "${S3_SECRET_KEY:-}" ]; then
  DATA_EXISTS=$(docker run --rm \
    -v "libsql-dockerized_primary_data:/data" \
    alpine sh -c 'test -f /data/dbs/default/data && echo yes || echo no' 2>/dev/null || echo no)
  if [ "$DATA_EXISTS" = "no" ]; then
    echo "Data dir empty — checking S3 for backups..."
    S3_BUCKET="${S3_BUCKET:-libsql-backups}"
    S3_PREFIX="${S3_PREFIX:-libsql}"
    S3_ENDPOINT="${S3_ENDPOINT:-s3.us-east-005.backblazeb2.com}"
    LATEST=$(docker run --rm --entrypoint sh \
      minio/mc:latest -c "
mc alias set s3host https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 2>/dev/null &&
mc ls s3host/$S3_BUCKET/$S3_PREFIX/ --json 2>/dev/null | \
  grep -o '\"key\":\"[^\"]*\.tar\.gz\"' | sed 's/\"key\":\"//;s/\"$//' | sort -r | head -1 | xargs -r basename
" 2>&1 | grep "libsql-" | tail -1 || true)
    if [ -n "$LATEST" ]; then
      echo "  Latest backup: $LATEST"
      docker run --rm --entrypoint sh \
        -v "libsql-dockerized_primary_data:/data" \
        -v "$(realpath ./backups):/backup" \
        minio/mc:latest -c "
mc alias set s3host https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 2>/dev/null &&
mc cp s3host/$S3_BUCKET/$S3_PREFIX/$LATEST /backup/$LATEST 2>/dev/null &&
rm -rf /data/data.sqld &&
mkdir -p /data &&
tar xzf /backup/$LATEST -C /data &&
chown -R 666:666 /data/data.sqld &&
echo '  ✓ Restored: $LATEST'
" 2>&1 | grep -v "^Alias"
    else
      echo "  No backups found in S3 — starting fresh"
    fi
  else
    echo "  Data exists — skipping S3 restore (idempotent)"
  fi
fi
# --- end DR ---

echo "Starting sqld cluster..."
docker compose up -d sqld-primary sqld-replica sqld-replica-2 2>&1 | tail -1

echo -n "Waiting for primary..."
until HC_PRIM; do sleep 2; done
echo " OK"

echo -n "Waiting for replica-1..."
until HC_REP; do sleep 2; done
echo " OK"

echo -n "Waiting for replica-2..."
until HC_REP2; do sleep 2; done
echo " OK"

echo "Starting controller + router..."
# The compose references pinned images; build them once if missing (e.g. on a
# fresh node). Re-applies on an existing node skip the build — instant.
if ! docker image inspect libsql-dockerized-controller:v0.24.33 >/dev/null 2>&1; then
  echo "  Building controller image (first deploy)..."
  docker build -t libsql-dockerized-controller:v0.24.33 -f controller/Dockerfile . 2>&1 | tail -2
fi
if ! docker image inspect libsql-dockerized-router:v0.24.33 >/dev/null 2>&1; then
  echo "  Building router image (first deploy)..."
  docker build -t libsql-dockerized-router:v0.24.33 -f router/Dockerfile . 2>&1 | tail -2
fi
docker compose up -d controller router 2>&1 | tail -1

echo -n "Waiting for controller..."
sleep 3
until HC_CTRL; do sleep 2; done
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
echo "  Controller: curl http://localhost:${CONTROLLER_PORT:-9090}/leader"
echo "  Backup: bash backup.sh"
echo "  Restore: bash restore.sh --help"
