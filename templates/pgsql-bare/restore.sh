#!/bin/sh
# pgsql-bare restore — restore the database from the latest dump on S3 (external
# B2) or local. `-y` skips the prompt.
# Secret env: S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY.
set -e

PG_APP_USER="${PG_APP_USER:-dev}"
PG_APP_PASSWORD="${PG_APP_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"
LISTEN_PORT="${PG_LISTEN_PORT:-5432}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_BUCKET="${S3_BUCKET:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
LOCAL_DIR="backups"
AUTO=""
[ "$1" = "-y" ] || [ "$1" = "--yes" ] && AUTO="-y"
if [ -z "$AUTO" ]; then
  echo "Restore will drop and recreate database '$PG_DATABASE'. Continue? (y/N)"
  read -r ans || true
  case "$ans" in y|Y) ;; *) echo "aborted"; exit 1 ;; esac
fi

echo "=== pgsql-bare restore ==="

FNAME=""
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  LIST=$(docker run --rm --entrypoint sh minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc ls --json s3/$S3_BUCKET/pgsql-bare/
  ")
  FNAME=$(echo "$LIST" | grep -o '"key": *"[^"]*"' | sed 's/.*"key": *"//; s/"$//' | sort | tail -1)
  [ -n "$FNAME" ] || { echo "no backups on S3"; exit 1; }
  mkdir -p "$LOCAL_DIR"
  echo "  downloading s3://$S3_BUCKET/pgsql-bare/$FNAME"
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup" minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc cp s3/$S3_BUCKET/pgsql-bare/$FNAME /backup/$FNAME
  "
else
  FNAME=$(ls -1t "$LOCAL_DIR"/*.sql.gz 2>/dev/null | head -1 | xargs -n1 basename 2>/dev/null || true)
  [ -n "$FNAME" ] || { echo "no local backups"; exit 1; }
fi
echo "  restoring: $FNAME"

# Terminate sessions + drop/recreate via a maintenance DB, then load.
su -s /bin/sh postgres -c "psql -h 127.0.0.1 -p $LISTEN_PORT -U postgres -d template1 -c \"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$PG_DATABASE' AND pid <> pg_backend_pid()\"" >/dev/null 2>&1 || true
su -s /bin/sh postgres -c "psql -h 127.0.0.1 -p $LISTEN_PORT -U postgres -d template1 -c 'DROP DATABASE IF EXISTS $PG_DATABASE'"
su -s /bin/sh postgres -c "psql -h 127.0.0.1 -p $LISTEN_PORT -U postgres -d template1 -c 'CREATE DATABASE $PG_DATABASE OWNER $PG_APP_USER'"
su -s /bin/sh postgres -c "psql -h 127.0.0.1 -p $LISTEN_PORT -U postgres -d $PG_DATABASE -c 'GRANT ALL ON SCHEMA public TO $PG_APP_USER'" || true
gunzip -c "$LOCAL_DIR/$FNAME" | PGPASSWORD="$PG_APP_PASSWORD" psql -h 127.0.0.1 -p "$LISTEN_PORT" -U "$PG_APP_USER" -d "$PG_DATABASE"

echo "  restore complete"
