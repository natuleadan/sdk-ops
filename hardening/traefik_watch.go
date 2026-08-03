package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// traefikWatchScript verifies the Traefik reverse proxy every run and
// self-heals what a timer can: a stopped container is started, the main
// config presence is checked, and acme.json permissions are kept at 0600.
// If the container is missing entirely it only notifies (the provisioning
// CLI owns the container). No daemon — a systemd timer runs it.
const traefikWatchScript = `#!/bin/bash
# sdk-ops traefik watchdog — keeps the reverse proxy alive
# Runs every 5 minutes. Docker's restart policy covers crashes; this covers
# stopped containers, missing config and broken acme.json permissions.
LOG=/var/log/sdk-ops-traefik.log
CFG=/etc/traefik/traefik.yml
ACME=/opt/traefik/acme.json

[ -f /etc/sdk-ops/firewall/notify.env ] && . /etc/sdk-ops/firewall/notify.env

log() { echo "[$(date -u +%FT%TZ)] $*" >> "$LOG"; }

notify() {
  [ -n "${TELEGRAM_API_KEY:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ] || return 0
  curl -fsS --connect-timeout 5 --max-time 10 "https://api.telegram.org/bot$TELEGRAM_API_KEY/sendMessage" \
    --data-urlencode "chat_id=$TELEGRAM_CHAT_ID" \
    --data-urlencode "text=$1" >/dev/null 2>&1 || true
}

if ! docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx 'traefik'; then
  log "container missing"
  notify "🚨 $(hostname): traefik container is missing — run sdk-ops infra provision"
  exit 0
fi

if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -qx 'traefik'; then
  sudo docker start traefik >/dev/null 2>&1 && log "container restarted" || log "start FAILED"
  exit 0
fi

if [ ! -f "$CFG" ]; then
  log "config missing: $CFG"
  notify "🚨 $(hostname): traefik config missing — run sdk-ops infra provision"
  exit 0
fi

if [ -f "$ACME" ]; then
  P=$(stat -c %a "$ACME" 2>/dev/null || echo 0)
  if [ "$P" != "600" ]; then
    sudo chmod 600 "$ACME" && log "acme.json perms fixed ($P -> 600)"
  fi
fi

log "ok"
`

// InstallTraefikWatch installs the traefik watchdog and its 5-minute systemd
// timer on the node.
func InstallTraefikWatch(client *goss.Client) error {
	script := `
sudo mkdir -p /opt/sdk-ops/traefik
sudo tee /opt/sdk-ops/traefik/watch.sh > /dev/null << 'SCRIPTEOF'
` + traefikWatchScript + `
SCRIPTEOF
sudo chown sdkops:sdkops /opt/sdk-ops/traefik/watch.sh
sudo chmod 0750 /opt/sdk-ops/traefik/watch.sh
sudo tee /etc/systemd/system/sdk-ops-traefik.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=sdk-ops traefik watchdog

[Service]
Type=oneshot
User=sdkops
ExecStart=/opt/sdk-ops/traefik/watch.sh
SERVICEEOF
sudo tee /etc/systemd/system/sdk-ops-traefik.timer > /dev/null << 'TIMEREOF'
[Unit]
Description=sdk-ops traefik watch timer (every 5 min)

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
TIMEREOF
sudo touch /var/log/sdk-ops-traefik.log 2>/dev/null || true
sudo chown sdkops:sdkops /var/log/sdk-ops-traefik.log 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl enable --now sdk-ops-traefik.timer 2>/dev/null
echo "traefik watch: installed (every 5 min)"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("traefik watch install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// RemoveTraefikWatch removes the traefik watchdog and its timer.
func RemoveTraefikWatch(client *goss.Client) error {
	script := `
sudo systemctl stop sdk-ops-traefik.timer 2>/dev/null || true
sudo systemctl disable sdk-ops-traefik.timer 2>/dev/null || true
sudo rm -f /etc/systemd/system/sdk-ops-traefik.service /etc/systemd/system/sdk-ops-traefik.timer
sudo rm -rf /opt/sdk-ops/traefik
sudo systemctl daemon-reload
echo "traefik watch removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("traefik watch remove: %w", err)
	}
	fmt.Print(out)
	return nil
}

// TraefikWatchStatus reports the watchdog state.
func TraefikWatchStatus(client *goss.Client) (string, error) {
	script := `
systemctl list-timers sdk-ops-traefik.timer --no-pager 2>/dev/null | grep sdk-ops-traefik || echo "timer not installed"
tail -2 /var/log/sdk-ops-traefik.log 2>/dev/null || echo "no events logged"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("traefik watch status: %w", err)
	}
	return strings.TrimSpace(out), nil
}
