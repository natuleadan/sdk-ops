#!/bin/sh
# yuga-bare restore — restore the app database from the latest dump on S3
# (external B2) or local. `-y` skips the prompt.
# Secret env: S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY.
set -e

YB_INSTALL_DIR="${YB_INSTALL_DIR:-/opt/yugabyte}"
YB_RELEASE="${YB_RELEASE:-2026.1.1.1}"
YB_DATA_DIR="${YB_DATA_DIR:-/var/lib/yugabyte}"
YB_DB="${YB_DB:-yugabyte}"
YB_USER="${YB_USER:-dev}"
ADDR="${YB_ADVERTISE:-127.0.0.1}"
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

echo "=== yuga-bare restore ==="

FNAME=""
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  # List JSON on the host (the mc image has no sed/awk) and take the newest.
  LIST=$(docker run --rm --entrypoint sh minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc ls --json s3/$S3_BUCKET/yuga-bare/
  ")
  FNAME=$(echo "$LIST" | grep -o '"key": *"[^"]*"' | sed 's/.*"key": *"//; s/"$//' | sort | tail -1)
  [ -n "$FNAME" ] || { echo "no backups on S3"; exit 1; }
  mkdir -p "$LOCAL_DIR"
  echo "  downloading s3://$S3_BUCKET/yuga-bare/$FNAME"
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup" minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc cp s3/$S3_BUCKET/yuga-bare/$FNAME /backup/$FNAME
  "
else
  FNAME=$(ls -1t "$LOCAL_DIR"/*.sql.gz 2>/dev/null | head -1 | xargs -n1 basename 2>/dev/null || true)
  [ -n "$FNAME" ] || { echo "no local backups"; exit 1; }
fi
echo "  restoring: $FNAME"

YSQLSH="$YB_INSTALL_DIR/yugabyte-$YB_RELEASE/bin/ysqlsh"
# System DB `yugabyte` never DROP — TRUNCATE instead (issues #5651, #4938).
# For app DBs, terminate sessions + DROP/CREATE via template1.
if [ "$YB_DB" = "yugabyte" ]; then
  echo "  system DB yugabyte — TRUNCATE tables (no DROP DATABASE)"
  su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U $YB_USER -d yugabyte -t -A -c \"SELECT 'TRUNCATE TABLE ' || quote_ident(tablename) || ' CASCADE;' FROM pg_tables WHERE schemaname='public'\" 2>/dev/null | su -s /bin/sh yugabyte -c \"$YSQLSH -h $ADDR -p 5433 -U $YB_USER -d yugabyte\" 2>/dev/null" || true
else
  su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U yugabyte -d template1 -c \\\"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$YB_DB'\\\" >/dev/null 2>&1 || true"
  su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U yugabyte -d template1 -c 'DROP DATABASE IF EXISTS $YB_DB'"
  su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U yugabyte -d template1 -c 'CREATE DATABASE $YB_DB OWNER $YB_USER'"
fi
gunzip -c "$LOCAL_DIR/$FNAME" | su -s /bin/sh yugabyte -c "$YSQLSH -h $ADDR -p 5433 -U $YB_USER -d $YB_DB"

echo "  restore complete"
