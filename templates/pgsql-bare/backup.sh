#!/bin/sh
# pgsql-bare backup — logical dump of the database to S3-compatible storage
# (Backblaze B2 / external) via pg_dump. Retention keeps the last N.
# Secret env (never in the repo): S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY.
set -e

PG_VERSION="${PG_VERSION:-18}"
PG_APP_USER="${PG_APP_USER:-dev}"
PG_APP_PASSWORD="${PG_APP_PASSWORD:-devpass}"
PG_DATABASE="${PG_DATABASE:-postgres}"
LISTEN_PORT="${PG_LISTEN_PORT:-5432}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_BUCKET="${S3_BUCKET:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
RETENTION="${BACKUP_RETENTION:-7}"
STAMP="$(date +%Y%m%d-%H%M%S)"
FNAME="$PG_DATABASE-$STAMP.sql.gz"
LOCAL_DIR="backups"

mkdir -p "$LOCAL_DIR"
echo "=== pgsql-bare backup ==="

PGPASSWORD="$PG_APP_PASSWORD" pg_dump -h 127.0.0.1 -p "$LISTEN_PORT" -U "$PG_APP_USER" -d "$PG_DATABASE" | gzip > "$LOCAL_DIR/$FNAME"
echo "  dump: $LOCAL_DIR/$FNAME ($(du -h "$LOCAL_DIR/$FNAME" | cut -f1))"

if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  # External S3 (Backblaze B2) — never MinIO. The mc image entrypoint is `mc`;
  # run it with --entrypoint sh for multi-command scripts.
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup:ro" minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc cp /backup/$FNAME s3/$S3_BUCKET/pgsql-bare/$FNAME
  "
  echo "  -> uploaded to s3://$S3_BUCKET/pgsql-bare/$FNAME"
  # Retention: list JSON on the host (the mc image has no sed/awk).
  docker run --rm --entrypoint sh minio/mc:latest -c \
    "mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null && mc ls --json s3/$S3_BUCKET/pgsql-bare/" \
    | grep -o '"key": *"[^"]*"' | sed 's/.*"key": *"//; s/"$//' | sort \
    | head -n -$RETENTION \
    | while read -r f; do
        [ -n "$f" ] && docker run --rm --entrypoint sh minio/mc:latest -c \
          "mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null && mc rm s3/$S3_BUCKET/pgsql-bare/$f" && echo "  purge $f"
      done
else
  echo "  no S3 config — local dump only (set S3_* to ship to external storage)"
fi
echo "  backup complete"
