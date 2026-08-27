#!/bin/sh
# libsql-dockerized backup-s3 — backup data.sqld (tar.gz) + upload to S3 (Backblaze B2)
# Uses mc (MinIO client) inside Docker — no host tools needed.
set -e

PRIMARY_CONTAINER="libsql-dockerized-sqld-primary-1"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATE=$(date +%F-%H%M%S)
S3_BUCKET="${S3_BUCKET:-libsql-backups}"
S3_PREFIX="${S3_PREFIX:-libsql}"
S3_ENDPOINT="${S3_ENDPOINT:-s3.us-east-005.backblazeb2.com}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
MC_ALIAS="${MC_ALIAS:-s3host}"
RETENTION="${RETENTION:-7}"

echo "=== libsql-dockerized backup-s3 ==="

docker inspect "$PRIMARY_CONTAINER" >/dev/null 2>&1 || {
  echo "ERROR: container $PRIMARY_CONTAINER not found"
  exit 1
}

if [ -z "$S3_ACCESS_KEY" ] || [ -z "$S3_SECRET_KEY" ]; then
  echo "ERROR: S3_ACCESS_KEY and S3_SECRET_KEY must be set"
  exit 1
fi

mkdir -p "$BACKUP_DIR"

echo "--- Step 1: Consistent snapshot (tar of data.sqld) ---"
# sqld keeps its state in data.sqld/ (WAL frames + wallog + snapshots), not in
# a plain .db — copying only dbs/default/data gives an empty checkpoint. Tar the
# whole data dir for a restorable point-in-time snapshot.
DATA_DIR="/var/lib/sqld/data.sqld"
LOCAL_DB="$BACKUP_DIR/libsql-$DATE.tar.gz"

docker exec "$PRIMARY_CONTAINER" sh -c "test -d $DATA_DIR" || {
  echo "  WARN: data dir not found"
  exit 0
}

docker exec "$PRIMARY_CONTAINER" sh -c "cd /var/lib/sqld && tar czf - data.sqld" > "$LOCAL_DB" 2>/dev/null
echo "  Local: $LOCAL_DB ($(du -h "$LOCAL_DB" | cut -f1))"

echo "--- Step 2: Upload to S3 ---"
docker run --rm --entrypoint sh \
  -v "$(realpath "$BACKUP_DIR"):/backup:ro" \
  minio/mc:latest -c "
mc alias set $MC_ALIAS https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 &&
mc mb $MC_ALIAS/$S3_BUCKET --ignore-existing &&
mc cp /backup/libsql-$DATE.tar.gz $MC_ALIAS/$S3_BUCKET/$S3_PREFIX/libsql-$DATE.tar.gz &&
echo '  Uploaded: s3://$S3_BUCKET/$S3_PREFIX/libsql-$DATE.tar.gz'
" 2>&1 | grep -v "^Added" | grep -v "^Alias"

echo "--- Step 3: Retention (keep last $RETENTION) ---"
docker run --rm --entrypoint sh \
  minio/mc:latest -c "
mc alias set $MC_ALIAS https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 &&
mc ls $MC_ALIAS/$S3_BUCKET/$S3_PREFIX/ --json 2>/dev/null | \
  grep -o '\"key\":\"[^\"]*\.tar\.gz\"' | sed 's/\"key\":\"//;s/\"$//' | \
  sort -r | tail -n +$((RETENTION + 1)) | \
  while read -r key; do
    echo \"  Removing old: \$key\"
    mc rm $MC_ALIAS/\$key 2>/dev/null || true
  done
" 2>&1 | grep -v "^Alias" || true

rm -f "$LOCAL_DB"

echo ""
echo "=== backup-s3 complete ==="
echo "  S3: s3://$S3_BUCKET/$S3_PREFIX/libsql-$DATE.tar.gz"
echo "  Restore: bash restore-s3.sh"
