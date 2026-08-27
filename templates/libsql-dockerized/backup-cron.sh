#!/bin/sh
# libsql-dockerized backup-cron — install daily backup cron to S3
set -e

CRON_SCHEDULE="${CRON_SCHEDULE:-0 4 * * *}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKUP_SCRIPT="$SCRIPT_DIR/backup-s3.sh"

echo "=== libsql-dockerized backup-cron ==="
echo "  Schedule: $CRON_SCHEDULE"
echo "  Script:   $BACKUP_SCRIPT"
echo "  S3:       s3://${S3_BUCKET:-libsql-backups}/${S3_PREFIX:-libsql}/"

if [ ! -f "$BACKUP_SCRIPT" ]; then
  echo "ERROR: $BACKUP_SCRIPT not found"
  exit 1
fi

cmd="$CRON_SCHEDULE cd $SCRIPT_DIR && bash ./backup-s3.sh >> /var/log/libsql-backup.log 2>&1"
(crontab -l 2>/dev/null | grep -v "backup-s3.sh"; echo "$cmd") | crontab -

echo "  ✓ Cron installed"
echo "  Logs: /var/log/libsql-backup.log"
echo "  Verify: crontab -l"
