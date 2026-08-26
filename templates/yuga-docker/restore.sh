#!/bin/sh
# yuga-docker restore — restore the app database from the latest dump
# on S3 (or local). Idempotent-ish: `-y` skips the confirmation prompt.
# Secret env: S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY.
set -e

NS="${PROVISION_NS:-yuga-docker}"
YB_DB="${YB_DB:-yugabyte}"
YB_USER="${YB_USER:-dev}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_BUCKET="${S3_BUCKET:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
LOCAL_DIR="backups"
AUTO=""

[ "$1" = "-y" ] || [ "$1" = "--yes" ] && AUTO="-y"
if [ -z "$AUTO" ]; then
  echo "Restore will drop and recreate database '$YB_DB'. Continue? (y/N)"
  read -r ans || true
  case "$ans" in y|Y) ;; *) echo "aborted"; exit 1 ;; esac
fi

echo "=== yuga-docker restore ==="

# Find the latest dump: prefer S3, fall back to local.
FNAME=""
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  # List the backups as JSON (the mc image has no sed/awk — parse the key on
  # the host with grep -o) and take the last (newest) filename.
  LIST=$(docker run --rm --entrypoint sh minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc ls --json s3/$S3_BUCKET/yugabyte/
  ")
  FNAME=$(echo "$LIST" | grep -o '"key": *"[^"]*"' | sed 's/.*"key": *"//; s/"$//' | sort | tail -1)
  if [ -z "$FNAME" ]; then echo "no backups on S3"; exit 1; fi
  mkdir -p "$LOCAL_DIR"
  echo "  downloading s3://$S3_BUCKET/yugabyte/$FNAME"
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup" minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc cp s3/$S3_BUCKET/yugabyte/$FNAME /backup/$FNAME
  "
else
  FNAME=$(ls -1t "$LOCAL_DIR"/*.sql.gz 2>/dev/null | head -1 | xargs -n1 basename 2>/dev/null || true)
  [ -n "$FNAME" ] || { echo "no local backups"; exit 1; }
fi

echo "  restoring: $FNAME"

# Drop + recreate the database. Connect to another DB (template1) and terminate
# any sessions still on the target so the DROP works on an active cluster.
YBH="${YB0_IP:-203.0.113.10}"
docker compose exec -T yugabyte-0 bash -lc "
  /usr/local/bin/ysqlsh -h $YBH -p 5433 -U yugabyte -d template1 -v ON_ERROR_STOP=1 -c \\
    \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$YB_DB' AND pid <> pg_backend_pid()\" >/dev/null 2>&1;
  /usr/local/bin/ysqlsh -h $YBH -p 5433 -U yugabyte -d template1 -v ON_ERROR_STOP=1 -c 'DROP DATABASE IF EXISTS $YB_DB' &&
  /usr/local/bin/ysqlsh -h $YBH -p 5433 -U yugabyte -d template1 -v ON_ERROR_STOP=1 -c 'CREATE DATABASE $YB_DB OWNER $YB_USER'
"
# Load the dump: pipe it into a file inside the container, then ysqlsh -f
# (a direct piped stdin to ysqlsh corrupts the COPY-from-stdin blocks).
gunzip -c "$LOCAL_DIR/$FNAME" | docker compose exec -T yugabyte-0 bash -lc \
  "cat > /tmp/restore.sql && /usr/local/bin/ysqlsh -h $YBH -p 5433 -U $YB_USER -d $YB_DB -f /tmp/restore.sql && rm -f /tmp/restore.sql"

echo "  restore complete"
