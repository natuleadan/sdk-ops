#!/bin/bash
# nats-bare backup cron wrapper - one-shot for the systemd timer (or cron).
# Called by a daily nats-backup.timer (OnCalendar=daily).
cd /opt/sdk-ops/services/nats-bare || exit 1
exec bash backup.sh
