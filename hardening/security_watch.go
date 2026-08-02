package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// securityWatchScript scans sshd journald for brute force attempts and sends
// a Telegram alert per offending IP (IP + attempt count + provider), plus a
// DDoS alert. It only notifies when there IS evidence in the window.
const securityWatchScript = `#!/bin/bash
# sdk-ops security watch — SSH brute force notifications
# Runs every 5 minutes; notifies ONLY when attempts are detected.
LOG=/var/log/sdk-ops-security.log
SENT_STATE=/etc/sdk-ops/security/sent
WINDOW=10
THRESHOLD=${SECURITY_THRESHOLD:-5}

[ -f /etc/sdk-ops/firewall/notify.env ] && . /etc/sdk-ops/firewall/notify.env

log() { echo "[$(date -u +%FT%TZ)] $*" >> "$LOG"; }

notify() {
  [ -n "${TELEGRAM_API_KEY:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ] || return 0
  curl -fsS --connect-timeout 5 --max-time 10 "https://api.telegram.org/bot$TELEGRAM_API_KEY/sendMessage" \
    --data-urlencode "chat_id=$TELEGRAM_CHAT_ID" \
    --data-urlencode "text=$1" >/dev/null 2>&1 || true
}

OUT=$(journalctl -u ssh.service -u sshd.service --since "${WINDOW} minutes ago" --no-pager -o short-iso 2>/dev/null)
[ -z "$OUT" ] && exit 0

IPS=$( { echo "$OUT" | grep -oE 'Failed (password|publickey) for [^ ]+ from [0-9a-fA-F:.]+' | grep -oE '[0-9a-fA-F:.]+$'
        echo "$OUT" | grep -oE 'Invalid user [^ ]+ from [0-9a-fA-F:.]+' | grep -oE '[0-9a-fA-F:.]+$'; } | sort -u )

[ -z "$IPS" ] && exit 0

TOTAL=0
for ip in $IPS; do
  C=$( { echo "$OUT" | grep -oE 'Failed (password|publickey) for [^ ]+ from '"$ip" | wc -l
         echo "$OUT" | grep -oE 'Invalid user [^ ]+ from '"$ip" | wc -l; } | awk '{s+=$1} END{print s}')
  TOTAL=$((TOTAL + C))
done

# DDoS: more than 50 unique IPs or 100 total attempts in the window
NUNIQ=$(echo "$IPS" | wc -l)
if [ "$NUNIQ" -gt 50 ] || [ "$TOTAL" -gt 100 ]; then
  notify "⚠️ DDoS $(hostname): $NUNIQ unique IPs, $TOTAL attempts in ${WINDOW}min"
  log "DDoS: $NUNIQ IPs, $TOTAL attempts"
fi

mkdir -p /etc/sdk-ops/security
NOW=$(date +%s)
for ip in $IPS; do
  C=$( { echo "$OUT" | grep -oE 'Failed (password|publickey) for [^ ]+ from '"$ip" | wc -l
         echo "$OUT" | grep -oE 'Invalid user [^ ]+ from '"$ip" | wc -l; } | awk '{s+=$1} END{print s}')
  [ "$C" -lt "$THRESHOLD" ] && continue
  LAST=0
  [ -f "$SENT_STATE" ] && LAST=$(grep "^$ip " "$SENT_STATE" 2>/dev/null | awk '{print $2}' | tail -1)
  [ -z "$LAST" ] && LAST=0
  if [ $((NOW - LAST)) -lt 3600 ]; then
    continue
  fi
  # provider (org) only — no geo country
  PROV=$(curl -fsS --max-time 5 "http://ip-api.com/line/$ip?fields=org" 2>/dev/null | tr -d '\r')
  [ -z "$PROV" ] && PROV="unknown"
  notify "🔐 $(hostname): $ip — $C attempts in ${WINDOW}min — $PROV"
  log "notified $ip ($C attempts, provider $PROV)"
  echo "$ip $NOW" >> "$SENT_STATE"
done
`

// SecurityWatchConfig configures the SSH brute force notifier.
type SecurityWatchConfig struct {
	Enabled   bool
	Threshold int
}

// InstallSecurityWatch installs the security watcher script and the 5-minute
// systemd timer on the node.
func InstallSecurityWatch(client *goss.Client, cfg SecurityWatchConfig) error {
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = 5
	}
	script := fmt.Sprintf(`
sudo mkdir -p /etc/sdk-ops/security /opt/sdk-ops/security
sudo tee /opt/sdk-ops/security/watch.sh > /dev/null << 'SCRIPTEOF'
%[1]s
SCRIPTEOF
sudo chmod 0750 /opt/sdk-ops/security/watch.sh
echo 'SECURITY_THRESHOLD=%[2]d' | sudo tee /etc/sdk-ops/security/env > /dev/null
sudo tee /etc/systemd/system/sdk-ops-security.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=sdk-ops SSH brute force watcher

[Service]
Type=oneshot
EnvironmentFile=/etc/sdk-ops/security/env
ExecStart=/opt/sdk-ops/security/watch.sh
SERVICEEOF
sudo tee /etc/systemd/system/sdk-ops-security.timer > /dev/null << 'TIMEREOF'
[Unit]
Description=sdk-ops security watch timer (every 5 min)

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
TIMEREOF
sudo systemctl daemon-reload
sudo systemctl enable --now sdk-ops-security.timer 2>/dev/null
echo "security watch: installed (threshold %[2]d, every 5 min)"
`, securityWatchScript, threshold)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("security watch install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// RemoveSecurityWatch removes the security watcher and its timer.
func RemoveSecurityWatch(client *goss.Client) error {
	script := `
sudo systemctl stop sdk-ops-security.timer 2>/dev/null || true
sudo systemctl disable sdk-ops-security.timer 2>/dev/null || true
sudo rm -f /etc/systemd/system/sdk-ops-security.service /etc/systemd/system/sdk-ops-security.timer
sudo rm -rf /opt/sdk-ops/security /etc/sdk-ops/security
sudo systemctl daemon-reload
echo "security watch removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("security watch remove: %w", err)
	}
	fmt.Print(out)
	return nil
}

// SecurityWatchStatus reports the watcher state.
func SecurityWatchStatus(client *goss.Client) (string, error) {
	script := `
systemctl list-timers sdk-ops-security.timer --no-pager 2>/dev/null | grep sdk-ops-security || echo "timer not installed"
tail -3 /var/log/sdk-ops-security.log 2>/dev/null || echo "no events logged"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("security watch status: %w", err)
	}
	return strings.TrimSpace(out), nil
}
