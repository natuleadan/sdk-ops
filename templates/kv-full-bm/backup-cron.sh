#!/bin/sh
# kv-full-bm backup-cron — install daily backup cron
set -e

CRON_SCHEDULE="${CRON_SCHEDULE:-0 3 * * *}"
BACKUP_SCRIPT="$(cd "$(dirname "$0")" && pwd)/backup.sh"

echo "=== kv-full-bm backup-cron ==="
echo "  Schedule: $CRON_SCHEDULE"
echo "  Docker:   backup via docker exec (no host tools needed)"

cmd="$CRON_SCHEDULE cd $(dirname "$BACKUP_SCRIPT") && bash ./backup.sh >> /var/log/kv-full-bm-backup.log 2>&1"
(crontab -l 2>/dev/null; echo "$cmd") | crontab -

echo "  ✓ Cron installed"
echo "  Logs: /var/log/kv-full-bm-backup.log"
echo "  Verify: crontab -l"
