package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// stateWatchScript verifies the nftables allowlist state on every run and
// re-applies it when it diverges (missing sets, missing admin elements or a
// stale state file). Fail-open: it never empties sets; on repair failure it
// only notifies. No daemon — a systemd timer runs it.
const stateWatchScript = `#!/bin/bash
# sdk-ops firewall state watchdog — keeps the allowlist tables intact
# Runs every 5 minutes; repairs and notifies ONLY with evidence.
LOG=/var/log/sdk-ops-state.log
STATE_FILE=/etc/sdk-ops/firewall/state
ADMIN4=/etc/sdk-ops/firewall/admin4.elements
ADMIN6=/etc/sdk-ops/firewall/admin6.elements
ALLOWLIST=/opt/sdk-ops/firewall/allowlist.sh
TABLE="inet filter"
STALE_AFTER=90000

[ -f /etc/sdk-ops/firewall/notify.env ] && . /etc/sdk-ops/firewall/notify.env

log() { echo "[$(date -u +%FT%TZ)] $*" >> "$LOG"; }

notify() {
  [ -n "${TELEGRAM_API_KEY:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ] || return 0
  curl -fsS --connect-timeout 5 --max-time 10 "https://api.telegram.org/bot$TELEGRAM_API_KEY/sendMessage"     --data-urlencode "chat_id=$TELEGRAM_CHAT_ID"     --data-urlencode "text=$1" >/dev/null 2>&1 || true
}

set_exists() { sudo nft list set "$TABLE" "$1" >/dev/null 2>&1; }

REASON=""

# 1. The four allowlist sets must exist.
for S in allow4 allow6 admin4 admin6; do
  if ! set_exists "$S"; then
    REASON="${REASON} set $S missing"
  fi
done

# 2. Every admin element must still be present (operator egress IPs).
for F in "$ADMIN4" "$ADMIN6"; do
  [ -f "$F" ] || continue
  S=admin4
  echo "$F" | grep -q 6 && S=admin6
  while read -r e; do
    [ -z "$e" ] && continue
    BARE=${e%%/*}
    if ! sudo nft get element "$TABLE" "$S" "{ $BARE }" >/dev/null 2>&1; then
      REASON="${REASON} admin element $e missing from $S"
    fi
  done < "$F"
done

# 3. The state file must be fresh (allowlist.sh must have run recently).
if [ ! -f "$STATE_FILE" ]; then
  REASON="${REASON} state file missing"
else
  LAST=$(stat -c %Y "$STATE_FILE" 2>/dev/null || echo 0)
  NOW=$(date +%s)
  if [ $((NOW - LAST)) -gt "$STALE_AFTER" ]; then
    REASON="${REASON} state file stale ($(date -u -d @$LAST +%FT%TZ 2>/dev/null))"
  fi
fi

if [ -z "$REASON" ]; then
  log "ok"
  exit 0
fi

log "diverged: $REASON"

# Notify cooldown: same divergence alerts at most once per hour.
NOTIFY_STATE=/etc/sdk-ops/firewall/state-notify
HASH=$(echo "$REASON" | md5sum | cut -d' ' -f1)
NOW=$(date +%s)
LAST=0
[ -f "$NOTIFY_STATE" ] && LAST=$(grep "^$HASH " "$NOTIFY_STATE" 2>/dev/null | awk '{print $2}' | tail -1)
[ -z "$LAST" ] && LAST=0
COOLDOWN=3600

# Repair: recreate missing sets first (allowlist.sh only flushes existing
# sets), then re-run the refresh which also self-heals admin4/admin6.
for S in allow4 allow6 admin4 admin6; do
  if ! set_exists "$S"; then
    case "$S" in
      allow4|admin4) TYPE=ipv4_addr ;;
      allow6|admin6) TYPE=ipv6_addr ;;
    esac
    sudo nft add set "$TABLE" "$S" "{ type $TYPE; flags interval; }" >/dev/null 2>&1       && log "recreated set $S" || log "FAILED to create set $S"
  fi
done

if [ -x "$ALLOWLIST" ] && sudo "$ALLOWLIST" >/dev/null 2>&1; then
  STILL=""
  for S in allow4 allow6 admin4 admin6; do
    set_exists "$S" || STILL="${STILL} $S still missing"
  done
  for F in "$ADMIN4" "$ADMIN6"; do
    [ -f "$F" ] || continue
    S=admin4
    echo "$F" | grep -q 6 && S=admin6
    while read -r e; do
      [ -z "$e" ] && continue
      BARE=${e%%/*}
      if ! sudo nft get element "$TABLE" "$S" "{ $BARE }" >/dev/null 2>&1; then
        STILL="${STILL} $e still missing from $S"
      fi
    done < "$F"
  done
  if [ $((NOW - LAST)) -lt "$COOLDOWN" ]; then
    log "repair done but notify suppressed (cooldown): $STILL"
  elif [ -z "$STILL" ]; then
    notify "🛡️ $(hostname): allowlist repaired — $REASON"
    log "repaired: re-ran allowlist.sh ($REASON)"
    echo "$HASH $NOW" | sudo tee -a "$NOTIFY_STATE" > /dev/null 2>&1 || true
  else
    notify "🚨 $(hostname): allowlist repair incomplete — $STILL"
    log "repair INCOMPLETE: $STILL"
    echo "$HASH $NOW" | sudo tee -a "$NOTIFY_STATE" > /dev/null 2>&1 || true
  fi
else
  if [ $((NOW - LAST)) -ge "$COOLDOWN" ]; then
    notify "🚨 $(hostname): allowlist repair FAILED — $REASON"
    echo "$HASH $NOW" | sudo tee -a "$NOTIFY_STATE" > /dev/null 2>&1 || true
  else
    log "repair failed but notify suppressed (cooldown): $REASON"
  fi
  log "repair FAILED: $REASON"
fi
`

// InstallStateWatch installs the firewall state watchdog and its 5-minute
// systemd timer on the node.
func InstallStateWatch(client *goss.Client) error {
	script := `
sudo mkdir -p /opt/sdk-ops/firewall
sudo tee /opt/sdk-ops/firewall/state_watch.sh > /dev/null << 'SCRIPTEOF'
` + stateWatchScript + `
SCRIPTEOF
sudo chown sdkops:sdkops /opt/sdk-ops/firewall/state_watch.sh
sudo chmod 0750 /opt/sdk-ops/firewall/state_watch.sh
sudo tee /etc/systemd/system/sdk-ops-state.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=sdk-ops firewall state watchdog

[Service]
Type=oneshot
User=sdkops
ExecStart=/opt/sdk-ops/firewall/state_watch.sh
SERVICEEOF
sudo tee /etc/systemd/system/sdk-ops-state.timer > /dev/null << 'TIMEREOF'
[Unit]
Description=sdk-ops firewall state watch timer (every 5 min)

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
TIMEREOF
sudo touch /var/log/sdk-ops-state.log 2>/dev/null || true
sudo chown sdkops:sdkops /var/log/sdk-ops-state.log 2>/dev/null || true
sudo usermod -aG systemd-journal sdkops 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl enable --now sdk-ops-state.timer 2>/dev/null
echo "state watch: installed (every 5 min)"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("state watch install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// RemoveStateWatch removes the firewall state watchdog and its timer.
func RemoveStateWatch(client *goss.Client) error {
	script := `
sudo systemctl stop sdk-ops-state.timer 2>/dev/null || true
sudo systemctl disable sdk-ops-state.timer 2>/dev/null || true
sudo rm -f /etc/systemd/system/sdk-ops-state.service /etc/systemd/system/sdk-ops-state.timer
sudo rm -f /opt/sdk-ops/firewall/state_watch.sh
sudo systemctl daemon-reload
echo "state watch removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("state watch remove: %w", err)
	}
	fmt.Print(out)
	return nil
}

// StateWatchStatus reports the watchdog state.
func StateWatchStatus(client *goss.Client) (string, error) {
	script := `
systemctl list-timers sdk-ops-state.timer --no-pager 2>/dev/null | grep sdk-ops-state || echo "timer not installed"
tail -2 /var/log/sdk-ops-state.log 2>/dev/null || echo "no events logged"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("state watch status: %w", err)
	}
	return strings.TrimSpace(out), nil
}
