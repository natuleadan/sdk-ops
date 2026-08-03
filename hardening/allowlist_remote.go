package hardening

import (
	"fmt"
	"strings"
	"time"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

const allowlistUpdaterScript = `#!/usr/bin/env bash
# sdk-ops firewall allowlist updater (managed by sdk-ops infra firewall)
# Fetches provider IP ranges and updates the nftables allow4/allow6 sets.
# Fail-open: never empties the sets when fetching or validation fails.
set -u

SOURCES_FILE=/etc/sdk-ops/firewall/sources
STATE_FILE=/etc/sdk-ops/firewall/state
LOG_FILE=/var/log/sdk-ops-allowlist.log
ADMIN4_FILE=/etc/sdk-ops/firewall/admin4.elements
ADMIN6_FILE=/etc/sdk-ops/firewall/admin6.elements
NOTIFY_ENV=/etc/sdk-ops/firewall/notify.env
MIN_CIDRS=4
MAX_DEPTH=10

log() { echo "[$(date -u +%FT%TZ)] $*" >> "$LOG_FILE"; }

# Optional Telegram alerting (configured via provision YAML -> notify.env)
[ -f "$NOTIFY_ENV" ] && . "$NOTIFY_ENV"
notify() {
  [ -n "${TELEGRAM_API_KEY:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ] || return 0
  curl -fsS --connect-timeout 5 --max-time 10 "https://api.telegram.org/bot$TELEGRAM_API_KEY/sendMessage" \
    --data-urlencode "chat_id=$TELEGRAM_CHAT_ID" \
    --data-urlencode "text=$1" >/dev/null 2>&1 || true
}

cidr4() {
  grep -Eq '^((25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(/(3[0-2]|[12]?[0-9]))?$' <<< "$1"
}

cidr6() {
  [[ "$1" == *:* ]] && grep -Eq '^[0-9a-fA-F:.]+(/[0-9]{1,3})?$' <<< "$1"
}

fetch_url() { curl -fsSL --connect-timeout 10 --max-time 30 "$1" 2>/dev/null; }

fetch_cf() {
  fetch_url https://www.cloudflare.com/ips-v4
  echo
  fetch_url https://www.cloudflare.com/ips-v6
}

declare -A VISITED
resolve_txt() {
  local fqdn=$1 depth=${2:-0}
  [ "$depth" -gt "$MAX_DEPTH" ] && return 1
  [ -n "${VISITED[$fqdn]:-}" ] && return 0
  VISITED[$fqdn]=1
  local recs tok
  recs=$(dig +short TXT "$fqdn" 2>/dev/null) || return 1
  for tok in $recs; do
    tok=${tok//\"/}
    case "$tok" in
      include:*) resolve_txt "${tok#include:}" "$((depth+1))" || return 1 ;;
      *) echo "$tok" ;;
    esac
  done
}

gather() {
  local line
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    case "$line" in
      cf) fetch_cf ;;
      url:*) fetch_url "${line#url:}" ;;
      dns:*) resolve_txt "${line#dns:}" 0 ;;
      *) continue ;;
    esac
  done < "$SOURCES_FILE"
}

# self_heal_admin restores admin sets from the files written at install time.
# The daily refresh never touches admin sets, but if they ever disappear this
# brings the operator bootstrap entries back before applying provider ranges.
self_heal_admin() {
  local v4 v6
  if [ -f "$ADMIN4_FILE" ]; then
    v4=$(tr '\n' ' ' < "$ADMIN4_FILE" | sed 's/  */ /g; s/^ //; s/ $//')
    [ -n "$v4" ] && {
      if ! sudo nft list set inet filter admin4 2>/dev/null | grep -q 'elements'; then
        sudo nft add element inet filter admin4 { $v4 } 2>/dev/null && log "self-heal: admin4 restored ($v4)"
      fi
    }
  fi
  if [ -f "$ADMIN6_FILE" ]; then
    v6=$(tr '\n' ' ' < "$ADMIN6_FILE" | sed 's/  */ /g; s/^ //; s/ $//')
    [ -n "$v6" ] && {
      if ! sudo nft list set inet filter admin6 2>/dev/null | grep -q 'elements'; then
        sudo nft add element inet filter admin6 { $v6 } 2>/dev/null && log "self-heal: admin6 restored ($v6)"
      fi
    }
  fi
}

update() {
  local tmp4 tmp6 fw line n4 n6
  tmp4=$(mktemp); tmp6=$(mktemp); fw=$(mktemp)
  while IFS= read -r line; do
    case "$line" in
      *:*:*|*::*) cidr6 "$line" && echo "$line" >> "$tmp6" ;;
      *) cidr4 "$line" && echo "$line" >> "$tmp4" ;;
    esac
  done < <(gather | tr '[:space:]' '\n' | sed '/^$/d' | sort -u)

  n4=$(wc -l < "$tmp4"); n6=$(wc -l < "$tmp6")
  if [ $((n4 + n6)) -lt "$MIN_CIDRS" ]; then
    log "ABORT: too few CIDRs (v4=$n4 v6=$n6, need >= $MIN_CIDRS)"
    notify "sdk-ops $(hostname): allowlist refresh ABORTED — too few CIDRs (v4=$n4 v6=$n6)"
    rm -f "$tmp4" "$tmp6" "$fw"
    exit 1
  fi

  {
    echo "flush set inet filter allow4"
    [ "$n4" -gt 0 ] && echo "add element inet filter allow4 { $(paste -sd, "$tmp4") }"
    echo "flush set inet filter allow6"
    [ "$n6" -gt 0 ] && echo "add element inet filter allow6 { $(paste -sd, "$tmp6") }"
  } > "$fw"

  if ! sudo nft -c -f "$fw" 2>/dev/null; then
    log "ABORT: nft parse failed"
    notify "sdk-ops $(hostname): allowlist refresh ABORTED — nft parse failed"
    rm -f "$tmp4" "$tmp6" "$fw"
    exit 1
  fi
  if ! sudo nft -f "$fw" 2>/dev/null; then
    log "ABORT: nft apply failed"
    notify "sdk-ops $(hostname): allowlist refresh ABORTED — nft apply failed"
    rm -f "$tmp4" "$tmp6" "$fw"
    exit 1
  fi

  sudo sh -c 'nft list table inet filter > /etc/nftables.conf' 2>/dev/null || true
  printf 'last_sync=%s\ncount_v4=%d\ncount_v6=%d\n' "$(date -u +%FT%TZ)" "$n4" "$n6" > "$STATE_FILE"
  log "updated v4=$n4 v6=$n6"
  echo "allowlist updated: v4=$n4 v6=$n6"
  rm -f "$tmp4" "$tmp6" "$fw"
}

self_heal_admin
update
`

// AllowlistUpdaterScript returns the updater script content (used by the ops
// CLI to compare the installed file against the template).
func AllowlistUpdaterScript() string { return allowlistUpdaterScript }

// InstallAllowlist applies the allowlist nftables config on the node and
// installs a systemd timer that refreshes the provider ranges daily.
//
// Safety: the previous live table is snapshotted for auto-rollback and a
// transient systemd-run rollback is armed before applying. The CLI must call
// CommitAllowlist after verifying a NEW SSH connection works; otherwise the
// node restores the previous state after the rollback window.
func InstallAllowlist(client *goss.Client, cfg AllowlistConfig) error {
	conf := GenerateAllowlistConfig(cfg)
	confFile := "/etc/sdk-ops/firewall/nftables.conf"
	sourcesFile := "/etc/sdk-ops/firewall/sources"
	admin4File := "/etc/sdk-ops/firewall/admin4.elements"
	admin6File := "/etc/sdk-ops/firewall/admin6.elements"

	installScript := fmt.Sprintf(`
set -e
sudo mkdir -p /etc/sdk-ops/firewall /opt/sdk-ops/firewall

# snapshot the current live table so the auto-rollback can restore it
sudo sh -c 'nft list table inet filter > /etc/sdk-ops/firewall/rollback.nft' 2>/dev/null || true

# keep the pre-feature config so allowlist remove can restore it
if [ -f /etc/nftables.conf ] && [ ! -f /etc/sdk-ops/firewall/nftables.conf.bak ]; then
  sudo cp /etc/nftables.conf /etc/sdk-ops/firewall/nftables.conf.bak
fi

sudo tee %[3]s > /dev/null << 'SRCEOF'
%[2]s
SRCEOF

sudo tee %[4]s > /dev/null << 'ADMIN4EOF'
%[6]s
ADMIN4EOF

sudo tee %[5]s > /dev/null << 'ADMIN6EOF'
%[7]s
ADMIN6EOF

sudo tee %[8]s > /dev/null << 'CONFEOF'
%[1]s
CONFEOF

# validate, then apply (flushing only our own table, never Docker tables)
sudo nft -c -f %[8]s || { echo "allowlist: config check failed" >&2; exit 1; }
sudo nft flush table inet filter 2>/dev/null || true
sudo nft -f %[8]s || {
  echo "allowlist: apply failed, rolling back" >&2
  sudo nft -f /etc/sdk-ops/firewall/rollback.nft 2>/dev/null || true
  exit 1
}
sudo cp %[8]s /etc/nftables.conf

# arm auto-rollback: if the CLI does not commit within 30s, restore previous state
sudo systemctl reset-failed sdk-ops-fw-rollback 2>/dev/null || true
sudo rm -f /tmp/sdk-ops-fw-commit
sudo systemd-run --on-active=30 --unit=sdk-ops-fw-rollback /bin/sh -c 'test -e /tmp/sdk-ops-fw-commit && exit 0; nft -f /etc/sdk-ops/firewall/rollback.nft 2>/dev/null || true' 2>/dev/null || true

sudo tee /opt/sdk-ops/firewall/allowlist.sh > /dev/null << 'SCRIPTEOF'
%[9]s
SCRIPTEOF
sudo chown sdkops:sdkops /opt/sdk-ops/firewall/allowlist.sh
sudo chmod 0750 /opt/sdk-ops/firewall/allowlist.sh

sudo tee /etc/systemd/system/sdk-ops-allowlist.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=sdk-ops firewall allowlist updater
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=sdkops
ExecStart=/opt/sdk-ops/firewall/allowlist.sh
SERVICEEOF
sudo tee /etc/systemd/system/sdk-ops-allowlist.timer > /dev/null << 'TIMEREOF'
[Unit]
Description=sdk-ops firewall allowlist refresh timer

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=300

[Install]
WantedBy=timers.target
TIMEREOF
sudo touch /var/log/sdk-ops-allowlist.log 2>/dev/null || true
sudo chown sdkops:sdkops /var/log/sdk-ops-allowlist.log 2>/dev/null || true
sudo usermod -aG systemd-journal sdkops 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl enable nftables 2>/dev/null || true
sudo systemctl enable --now sdk-ops-allowlist.timer 2>/dev/null
echo "allowlist: config applied, rollback armed, timer enabled"
`, conf, cfg.Source.String(), sourcesFile, admin4File, admin6File, strings.Join(cfg.Admin4, "\n"), strings.Join(cfg.Admin6, "\n"), confFile, allowlistUpdaterScript)

	out, _, err := ssh.Run(client, installScript)
	if err != nil {
		return fmt.Errorf("allowlist install: %w\n%s", err, out)
	}
	fmt.Print(out)

	// Seed the allow4/allow6 sets immediately.
	seedOut, _, seedErr := ssh.Run(client, `sudo /opt/sdk-ops/firewall/allowlist.sh`)
	if seedErr != nil {
		return fmt.Errorf("allowlist seed: %w\n%s", seedErr, seedOut)
	}
	fmt.Print(seedOut)

	// Re-apply exposed ports (the table flush reset the exposed chain).
	reapplyPortRegistry(client)
	return nil
}

// CommitAllowlist disarms the auto-rollback after a successful connectivity
// verification.
func CommitAllowlist(client *goss.Client) error {
	_, _, err := ssh.Run(client, `touch /tmp/sdk-ops-fw-commit && sudo systemctl stop sdk-ops-fw-rollback.timer sdk-ops-fw-rollback.service 2>/dev/null || true; echo committed`)
	if err != nil {
		return fmt.Errorf("allowlist commit: %w", err)
	}
	return nil
}

// RefreshAllowlist runs the updater script once on the node.
func RefreshAllowlist(client *goss.Client) error {
	out, _, err := ssh.Run(client, `sudo /opt/sdk-ops/firewall/allowlist.sh`)
	if err != nil {
		return fmt.Errorf("allowlist refresh: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// AllowlistStatus reports the last sync state and live set element counts.
func AllowlistStatus(client *goss.Client) (string, error) {
	script := `
echo "=== allowlist state ==="
cat /etc/sdk-ops/firewall/state 2>/dev/null || echo "no state (allowlist not installed?)"
echo ""
echo "=== recent updates ==="
tail -5 /var/log/sdk-ops-allowlist.log 2>/dev/null || true
echo ""
echo "=== timer ==="
systemctl list-timers sdk-ops-allowlist.timer --no-pager 2>/dev/null | grep -v '^$' || echo "timer not found"
echo ""
echo "=== live admin4 set ==="
sudo nft list set inet filter admin4 2>/dev/null || echo "set admin4 not found"
echo "=== live admin6 set ==="
sudo nft list set inet filter admin6 2>/dev/null || echo "set admin6 not found"
echo "=== live allow4 set ==="
sudo nft list set inet filter allow4 2>/dev/null || echo "set allow4 not found"
echo "=== live allow6 set ==="
sudo nft list set inet filter allow6 2>/dev/null || echo "set allow6 not found"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("allowlist status: %w", err)
	}
	return out, nil
}

// RemoveAllowlist restores the pre-allowlist firewall config and removes the
// updater script, timer, and rollback unit.
func RemoveAllowlist(client *goss.Client) error {
	script := `
set -e
sudo systemctl stop sdk-ops-allowlist.timer sdk-ops-fw-rollback.timer 2>/dev/null || true
sudo systemctl disable sdk-ops-allowlist.timer 2>/dev/null || true
sudo rm -f /etc/systemd/system/sdk-ops-allowlist.service /etc/systemd/system/sdk-ops-allowlist.timer
sudo rm -f /tmp/sdk-ops-fw-commit
sudo systemctl daemon-reload
if [ -f /etc/sdk-ops/firewall/nftables.conf.bak ]; then
  sudo nft flush table inet filter 2>/dev/null || true
  if sudo nft -c -f /etc/sdk-ops/firewall/nftables.conf.bak; then
    sudo nft -f /etc/sdk-ops/firewall/nftables.conf.bak
    sudo cp /etc/sdk-ops/firewall/nftables.conf.bak /etc/nftables.conf
  else
    echo "allowlist: backup restore failed, keeping current rules" >&2
  fi
else
  echo "allowlist: no backup found, leaving current nftables rules intact" >&2
fi
sudo rm -rf /etc/sdk-ops/firewall /opt/sdk-ops/firewall
sudo systemctl restart nftables 2>/dev/null || true
echo "allowlist removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("allowlist remove: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// AllowlistAdminAdd adds a permanent admin IP to the allowlist firewall.
func AllowlistAdminAdd(client *goss.Client, ip string) error {
	ip = NormalizeCIDR(ip)
	isV6, err := ValidateCIDR(ip)
	if err != nil {
		return err
	}
	set, elemFile := "admin4", "/etc/sdk-ops/firewall/admin4.elements"
	if isV6 {
		set, elemFile = "admin6", "/etc/sdk-ops/firewall/admin6.elements"
	}
	script := fmt.Sprintf(`
if ! sudo nft list table inet filter 2>/dev/null | grep -q 'set %[1]s'; then
  echo "allowlist: not installed (no %[1]s set)" >&2
  exit 1
fi
sudo nft add element inet filter %[1]s { %[2]s }
if ! grep -q '^%[2]s$' %[3]s 2>/dev/null; then echo %[2]s >> %[3]s; fi
sudo nft list table inet filter | sudo tee /etc/nftables.conf >/dev/null
echo "admin IP %[2]s added to %[1]s"
`, set, ip, elemFile)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("allowlist admin add: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// AllowlistAdminRemove removes a permanent admin IP from the allowlist firewall.
func AllowlistAdminRemove(client *goss.Client, ip string) error {
	ip = NormalizeCIDR(ip)
	isV6, err := ValidateCIDR(ip)
	if err != nil {
		return err
	}
	set, elemFile := "admin4", "/etc/sdk-ops/firewall/admin4.elements"
	if isV6 {
		set, elemFile = "admin6", "/etc/sdk-ops/firewall/admin6.elements"
	}
	script := fmt.Sprintf(`
if ! sudo nft list table inet filter 2>/dev/null | grep -q 'set %[1]s'; then
  echo "allowlist: not installed (no %[1]s set)" >&2
  exit 1
fi
sudo nft delete element inet filter %[1]s { %[2]s } 2>/dev/null || echo "admin IP %[2]s not present in %[1]s"
sed -i '/^%[2]s$/d' %[3]s 2>/dev/null || true
sudo nft list table inet filter | sudo tee /etc/nftables.conf >/dev/null
echo "done"
`, set, ip, elemFile)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("allowlist admin remove: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// AllowlistSources returns the installed sources file content.
func AllowlistSources(client *goss.Client) (string, error) {
	out, _, err := ssh.Run(client, `cat /etc/sdk-ops/firewall/sources 2>/dev/null || echo ""`)
	if err != nil {
		return "", fmt.Errorf("allowlist sources: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// PortScope describes who can reach an exposed port.
type PortScope string

const (
	// PortScopeAdmin restricts the port to the permanent admin IPs (v4+v6).
	PortScopeAdmin PortScope = "admin"
	// PortScopeGlobal opens the port to every IP.
	PortScopeGlobal PortScope = "global"
	// PortScopeTraefik registers the port as traefik-routed (no firewall rule).
	PortScopeTraefik PortScope = "traefik"
	// PortScopeIPs restricts the port to an explicit array of IPs/CIDRs.
	PortScopeIPs PortScope = "ips"
)

const portsRegistryPath = "/etc/sdk-ops/firewall/ports.yaml"

// buildExposeRules renders the nft rules for a scope and the registry IP
// field. Admin scope accepts from admin4/admin6 sets (v4+v6) then drops;
// global accepts all; ips emits one accept rule per IP then drops; traefik
// produces no rules.
func buildExposeRules(port int, proto string, scope PortScope, ips []string) (rules, ipsField string, err error) {
	protoExpr := proto + " dport"
	switch scope {
	case PortScopeAdmin:
		return fmt.Sprintf(`sudo nft add rule inet filter exposed %[1]s %[2]d ip saddr @admin4 accept
sudo nft add rule inet filter exposed %[1]s %[2]d ip6 saddr @admin6 accept
sudo nft add rule inet filter exposed %[1]s %[2]d drop`, protoExpr, port), "", nil
	case PortScopeGlobal:
		return fmt.Sprintf(`sudo nft add rule inet filter exposed %[1]s %[2]d accept`, protoExpr, port), "", nil
	case PortScopeTraefik:
		return "", "", nil
	case PortScopeIPs:
		if len(ips) == 0 {
			return "", "", fmt.Errorf("ips scope requires at least one IP via --ips")
		}
		var v4s, v6s []string
		for _, raw := range ips {
			ip := NormalizeCIDR(strings.TrimSpace(raw))
			if _, verr := ValidateCIDR(ip); verr != nil {
				return "", "", fmt.Errorf("invalid IP %q: %w", raw, verr)
			}
			if isV6, _ := ValidateCIDR(ip); isV6 {
				v6s = append(v6s, ip)
			} else {
				v4s = append(v4s, ip)
			}
		}
		var b strings.Builder
		for _, ip := range v4s {
			fmt.Fprintf(&b, "sudo nft add rule inet filter exposed %s %d ip saddr %s accept\n", protoExpr, port, ip)
		}
		for _, ip := range v6s {
			fmt.Fprintf(&b, "sudo nft add rule inet filter exposed %s %d ip6 saddr %s accept\n", protoExpr, port, ip)
		}
		fmt.Fprintf(&b, "sudo nft add rule inet filter exposed %s %d drop", protoExpr, port)
		return b.String(), strings.Join(append(v4s, v6s...), ","), nil
	default:
		return "", "", fmt.Errorf("unknown scope %q (admin, global, ips, traefik)", scope)
	}
}

// AllowlistExposePort opens a port under a scope and records it in the
// registry. Admin scope restricts to the admin4/admin6 sets (operator IPs,
// IPv4 and IPv6); global opens to all; ips restricts to an explicit IP list;
// traefik only registers the port.
func AllowlistExposePort(client *goss.Client, port int, proto string, scope PortScope, ips ...string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port out of range: %d", port)
	}
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("proto must be tcp or udp")
	}
	rules, ipsField, err := buildExposeRules(port, proto, scope, ips)
	if err != nil {
		return err
	}

	script := fmt.Sprintf(`
if ! sudo nft list table inet filter 2>/dev/null | grep -q 'set admin4'; then
  echo "allowlist: not installed" >&2
  exit 1
fi
# self-heal the exposed chain (older configs may lack it)
sudo nft add chain inet filter exposed { } 2>/dev/null || true
if ! sudo nft list chain inet filter input 2>/dev/null | grep -q 'jump exposed'; then
  sudo nft insert rule inet filter input jump exposed
fi
if ! sudo nft list chain inet filter forward 2>/dev/null | grep -q 'jump exposed'; then
  sudo nft insert rule inet filter forward jump exposed
fi
%[3]s
# dedup: one registry line per port/proto/scope, keep the freshest
sudo sed -i "/^%[2]d %[4]s %[5]s /d" %[1]s 2>/dev/null || true
echo "%[2]d %[4]s %[5]s %[6]s %[7]s" | sudo tee -a %[1]s > /dev/null
sudo nft list table inet filter | sudo tee /etc/nftables.conf > /dev/null
echo "port %[2]d/%[4]s exposed (%[5]s)"
`, portsRegistryPath, port, rules, proto, scope, ipsField, time.Now().UTC().Format(time.RFC3339))

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("allowlist expose %d: %w\n%s", port, err, out)
	}
	fmt.Print(out)
	return nil
}

// AllowlistUnexposePort closes a port and removes it from the registry.
func AllowlistUnexposePort(client *goss.Client, port int) error {
	// Word boundary after the port so "80" never matches rules for 8088.
	script := fmt.Sprintf(`
HANDLES=$(sudo nft --handle list chain inet filter exposed 2>/dev/null | grep -E "dport %[1]d([^0-9]|$)" | grep -oE 'handle [0-9]+' | awk '{print $2}')
for h in $HANDLES; do
  sudo nft delete rule inet filter exposed handle $h 2>/dev/null || true
done
sudo sed -i '/^%[1]d /d' %[2]s 2>/dev/null || true
sudo nft list table inet filter | sudo tee /etc/nftables.conf > /dev/null
echo "port %[1]d unexposed"
`, port, portsRegistryPath)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("allowlist unexpose %d: %w\n%s", port, err, out)
	}
	fmt.Print(out)
	return nil
}

// AllowlistPorts lists the exposed ports registry.
func AllowlistPorts(client *goss.Client) (string, error) {
	out, _, err := ssh.Run(client, `sudo cat `+portsRegistryPath+` 2>/dev/null || echo "no exposed ports"`)
	if err != nil {
		return "", fmt.Errorf("allowlist ports: %w", err)
	}
	return out, nil
}

// reapplyPortRegistry re-applies the exposed ports after a config reinstall
// (the table flush resets the exposed chain).
func reapplyPortRegistry(client *goss.Client) {
	out, _, err := ssh.Run(client, `sudo cat `+portsRegistryPath+` 2>/dev/null || true`)
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		port := 0
		if _, err := fmt.Sscanf(fields[0], "%d", &port); err != nil || port == 0 {
			continue
		}
		var ips []string
		if len(fields) > 3 && fields[3] != "" {
			ips = strings.Split(fields[3], ",")
		}
		_ = AllowlistExposePort(client, port, fields[1], PortScope(fields[2]), ips...)
	}
}
