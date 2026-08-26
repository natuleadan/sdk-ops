#!/bin/sh
# yuga-bare backup — logical dump of the app database to S3-compatible
# storage (Backblaze B2 / external) via ysql_dump. Retention keeps the last N.
# Secret env (never in the repo): S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY.
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
RETENTION="${BACKUP_RETENTION:-7}"
STAMP="$(date +%Y%m%d-%H%M%S)"
FNAME="$YB_DB-$STAMP.sql.gz"
LOCAL_DIR="backups"

mkdir -p "$LOCAL_DIR"
echo "=== yuga-bare backup ==="

DUMP_BIN="$YB_INSTALL_DIR/yugabyte-$YB_RELEASE/postgres/bin/ysql_dump"
su -s /bin/sh yugabyte -c "$DUMP_BIN -h $ADDR -p 5433 -U $YB_USER -d $YB_DB" | gzip > "$LOCAL_DIR/$FNAME"
echo "  dump: $LOCAL_DIR/$FNAME ($(du -h "$LOCAL_DIR/$FNAME" | cut -f1))"

if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  # External S3 (Backblaze B2) — never MinIO. The mc image entrypoint is `mc`;
  # run it with --entrypoint sh for multi-command scripts.
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup:ro" minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc cp /backup/$FNAME s3/$S3_BUCKET/yuga-bare/$FNAME
  "
  echo "  → uploaded to s3://$S3_BUCKET/yuga-bare/$FNAME"
  # Retention: list JSON on the host (the mc image has no sed/awk), keep the
  # newest RETENTION, purge the rest.
  docker run --rm --entrypoint sh minio/mc:latest -c \
    "mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null && mc ls --json s3/$S3_BUCKET/yuga-bare/" \
    | grep -o '"key": *"[^"]*"' | sed 's/.*"key": *"//; s/"$//' | sort \
    | head -n -$RETENTION \
    | while read -r f; do
        [ -n "$f" ] && docker run --rm --entrypoint sh minio/mc:latest -c \
          "mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null && mc rm s3/$S3_BUCKET/yuga-bare/$f" && echo "  purge $f"
      done
else
  echo "  no S3 config — local dump only (set S3_* to ship to external storage)"
fi
echo "  backup complete"
