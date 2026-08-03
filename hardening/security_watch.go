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
# sdk-ops security watch — brute force notifications, ALL ports, ranked
# Runs every 5 minutes. Notifies ONCE per window and ONLY when there were
# attempts. Sources: sshd journald (port 22) + kernel firewall drops
# (every other port, prefix "sdk-drop:"). Reported ordered by attempt count.
LOG=/var/log/sdk-ops-security.log
WINDOW=5
TOP=10

[ -f /etc/sdk-ops/firewall/notify.env ] && . /etc/sdk-ops/firewall/notify.env

log() { echo "[$(date -u +%FT%TZ)] $*" >> "$LOG"; }

notify() {
  [ -n "${TELEGRAM_API_KEY:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ] || return 0
  curl -fsS --connect-timeout 5 --max-time 10 "https://api.telegram.org/bot$TELEGRAM_API_KEY/sendMessage" \
    --data-urlencode "chat_id=$TELEGRAM_CHAT_ID" \
    --data-urlencode "text=$1" >/dev/null 2>&1 || true
}

OUT_S=$(journalctl -u ssh.service -u sshd.service --since "${WINDOW} minutes ago" --no-pager -o short-iso 2>/dev/null)
OUT_K=$(journalctl -k --since "${WINDOW} minutes ago" --no-pager -o short-iso 2>/dev/null | grep "sdk-drop:")
[ -z "$OUT_S" ] && [ -z "$OUT_K" ] && exit 0

declare -A CNT
declare -A PORTS

# 1. sshd attempts → port 22
while IFS= read -r ip; do
  [ -z "$ip" ] && continue
  CNT[$ip]=$(( ${CNT[$ip]:-0} + 1 ))
  PORTS[$ip]="${PORTS[$ip]} 22"
done < <(echo "$OUT_S" | grep -oE 'Failed (password|publickey) for [^ ]+ from [0-9a-fA-F:.]+|Invalid user [^ ]+ from [0-9a-fA-F:.]+' | grep -oE '[0-9a-fA-F:.]+$')

# 2. firewall drops → every other port (kernel log, prefix sdk-drop:)
while IFS= read -r line; do
  ip=$(echo "$line" | grep -oE 'SRC=[0-9a-fA-F:.]+' | cut -d= -f2)
  dpt=$(echo "$line" | grep -oE 'DPT=[0-9]+' | cut -d= -f2)
  [ -z "$ip" ] && continue
  [ -z "$dpt" ] && dpt=0
  CNT[$ip]=$(( ${CNT[$ip]:-0} + 1 ))
  PORTS[$ip]="${PORTS[$ip]} $dpt"
done < <(echo "$OUT_K")

[ ${#CNT[@]} -eq 0 ] && exit 0

TOTAL=0
for ip in "${!CNT[@]}"; do TOTAL=$((TOTAL + CNT[$ip])); done
NUNIQ=${#CNT[@]}

if [ "$NUNIQ" -gt 50 ] || [ "$TOTAL" -gt 100 ]; then
  notify "⚠️ DDoS $(hostname): $NUNIQ unique IPs, $TOTAL attempts in ${WINDOW}min"
  log "DDoS: $NUNIQ IPs, $TOTAL attempts"
fi

ALLPORTS=$(for ip in "${!CNT[@]}"; do echo ${PORTS[$ip]}; done | tr ' ' '\n' | grep -E '^[0-9]+$' | sort -n -u | paste -sd,)
TOPIPS=$(for ip in "${!CNT[@]}"; do echo "${CNT[$ip]} $ip"; done | sort -rn | head -n "$TOP" | awk '{print $2}')

MSG="🔐 $(hostname): $NUNIQ IPs / $TOTAL attempts in ${WINDOW}min — ports: $ALLPORTS"
for ip in $TOPIPS; do
  PB=$(echo ${PORTS[$ip]} | tr ' ' '\n' | grep -E '^[0-9]+$' | sort -n | uniq -c | sort -rn | awk '{printf "%s×%s ", $2, $1}' | sed 's/ $//')
  PROV=$(curl -fsS --max-time 5 "http://ip-api.com/line/$ip?fields=org" 2>/dev/null | tr -d '\r')
  [ -z "$PROV" ] && PROV="unknown"
  MSG="$MSG
$ip — ${CNT[$ip]} attempts ($PB) — $PROV"
done

notify "$MSG"
log "notified: $NUNIQ IPs, $TOTAL attempts, top: $TOPIPS, ports: $ALLPORTS"
`

// SecurityWatchScript returns the watcher script content (used by the ops
// CLI to compare the installed file against the template).
func SecurityWatchScript() string { return securityWatchScript }

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
sudo chown sdkops:sdkops /opt/sdk-ops/security/watch.sh
sudo chmod 0750 /opt/sdk-ops/security/watch.sh
echo 'SECURITY_THRESHOLD=%[2]d' | sudo tee /etc/sdk-ops/security/env > /dev/null
sudo tee /etc/systemd/system/sdk-ops-security.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=sdk-ops SSH brute force watcher

[Service]
Type=oneshot
User=sdkops
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
sudo touch /var/log/sdk-ops-security.log 2>/dev/null || true
sudo chown sdkops:sdkops /var/log/sdk-ops-security.log 2>/dev/null || true
sudo usermod -aG systemd-journal sdkops 2>/dev/null || true
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
