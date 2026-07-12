#!/bin/sh
# pg-full-bm backup-cron — install daily backup cron
set -e

CRON_SCHEDULE="${CRON_SCHEDULE:-0 3 * * *}"
BACKUP_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== pg-full-bm backup-cron ==="
echo "  Schedule: $CRON_SCHEDULE"
echo "  Docker:   backup via docker exec (no host tools needed)"

cmd="$CRON_SCHEDULE cd $BACKUP_DIR && bash ./backup.sh >> /var/log/pg-full-bm-backup.log 2>&1"
(crontab -l 2>/dev/null; echo "$cmd") | crontab -

echo "  ✓ Cron installed"
echo "  Logs: /var/log/pg-full-bm-backup.log"
echo "  Verify: crontab -l"
