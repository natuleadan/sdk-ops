#!/bin/sh
# yuga-docker backup — logical dump of the app database to S3-compatible
# storage (Backblaze B2 / MinIO) via pg_dump. Retention keeps the last N dumps.
# Secret env (never in the repo): S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY.
set -e

NS="${PROVISION_NS:-yuga-docker}"
YB_DB="${YB_DB:-yugabyte}"
YB_USER="${YB_USER:-dev}"
S3_ENDPOINT="${S3_ENDPOINT:-}"
S3_BUCKET="${S3_BUCKET:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
RETENTION="${BACKUP_RETENTION:-7}"
STAMP="$(date +%Y%m%d-%H%M%S)"
FNAME="$YB_DB-$STAMP.sql.gz"
LOCAL_DIR="backups"

mkdir -p "$LOCAL_DIR"

echo "=== yuga-docker backup ==="
echo "DB: $YB_DB  stamp: $STAMP"

# Logical dump through the primary YSQL endpoint (ysql_dump — the Postgres
# wire tool; ysqlsh lives at /usr/local/bin, the dump tools under the
# postgres bin dir inside the image).
DUMP_BIN=""
for c in /home/yugabyte/postgres/bin/ysql_dump /usr/local/bin/ysql_dump; do
  if docker compose exec -T yugabyte-0 bash -lc "[ -x $c ]"; then DUMP_BIN=$c; break; fi
done
[ -n "$DUMP_BIN" ] || { echo "ERROR: ysql_dump not found"; exit 1; }
docker compose exec -T yugabyte-0 bash -lc \
  "$DUMP_BIN -h ${YB0_IP:-203.0.113.10} -p 5433 -U $YB_USER -d $YB_DB" | gzip > "$LOCAL_DIR/$FNAME"
echo "  dump: $LOCAL_DIR/$FNAME ($(du -h "$LOCAL_DIR/$FNAME" | cut -f1))"

# Ship to S3 when configured.
if [ -n "$S3_ENDPOINT" ] && [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
  # The mc image entrypoint is `mc` — run sh explicitly for the script.
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup:ro" minio/mc:latest -c "
    mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null &&
    mc cp /backup/$FNAME s3/$S3_BUCKET/yugabyte/$FNAME
  "
  echo "  -> uploaded to s3://$S3_BUCKET/yugabyte/$FNAME"
  # Retention: list JSON on the host (the mc image has no sed/awk), keep the
  # newest RETENTION, purge the rest.
  docker run --rm --entrypoint sh -v "$(pwd)/$LOCAL_DIR:/backup:ro" minio/mc:latest -c \
    "mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null && mc ls --json s3/$S3_BUCKET/yugabyte/" \
    | grep -o '"key": *"[^"]*"' | sed 's/.*"key": *"//; s/"$//' | sort \
    | head -n -$RETENTION \
    | while read -r f; do
        [ -n "$f" ] && docker run --rm --entrypoint sh minio/mc:latest -c \
          "mc alias set s3 https://$S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY --api S3v4 >/dev/null && mc rm s3/$S3_BUCKET/yugabyte/$f" && echo "  purge $f"
      done
else
  echo "  no S3 config — local dump only (set S3_* to ship to storage)"
fi

echo "  backup complete"
