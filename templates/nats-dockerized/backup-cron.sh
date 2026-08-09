#!/bin/bash
# nats-dockerized backup cron wrapper — one-shot for the systemd timer.
# Called by nats-backup.service (OnCalendar=daily).
cd /opt/sdk-ops/services/nats || exit 1
exec bash backup.sh
