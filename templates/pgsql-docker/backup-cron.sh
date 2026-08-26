#!/bin/sh
# pgsql-docker backup-cron — install daily backup cron
set -e

CRON_SCHEDULE="${CRON_SCHEDULE:-0 3 * * *}"
BACKUP_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== pgsql-docker backup-cron ==="
echo "  Schedule: $CRON_SCHEDULE"
echo "  Docker:   backup via docker exec (no host tools needed)"

cmd="$CRON_SCHEDULE cd $BACKUP_DIR && bash ./backup.sh >> /var/log/pgsql-docker-backup.log 2>&1"
(crontab -l 2>/dev/null; echo "$cmd") | crontab -

echo "  ✓ Cron installed"
echo "  Logs: /var/log/pgsql-docker-backup.log"
echo "  Verify: crontab -l"
