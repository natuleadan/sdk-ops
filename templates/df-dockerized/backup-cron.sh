#!/bin/sh
# df-dockerized backup-cron — install daily backup cron to S3
set -e

CRON_SCHEDULE="${CRON_SCHEDULE:-0 3 * * *}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKUP_SCRIPT="$SCRIPT_DIR/backup-s3.sh"

echo "=== df-dockerized backup-cron ==="
echo "  Schedule: $CRON_SCHEDULE"
echo "  Script:   $BACKUP_SCRIPT"
echo "  S3:       s3://${S3_BUCKET:-df-backups}/${S3_PREFIX:-df}/"

if [ ! -f "$BACKUP_SCRIPT" ]; then
  echo "ERROR: $BACKUP_SCRIPT not found"
  exit 1
fi

cmd="$CRON_SCHEDULE cd $SCRIPT_DIR && bash ./backup-s3.sh >> /var/log/df-dockerized-backup.log 2>&1"
(crontab -l 2>/dev/null | grep -v "backup-s3.sh"; echo "$cmd") | crontab -

echo "  [OK] Cron installed"
echo "  Logs: /var/log/df-dockerized-backup.log"
echo "  Verify: crontab -l"
