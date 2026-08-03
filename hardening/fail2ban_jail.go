package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// Fail2banJailConfig renders the fail2ban jail.local owned by sdk-ops.
// The operator admin IPs are always ignored so a shared-NAT neighbor can
// never lock the operator out.
type Fail2banJailConfig struct {
	SSHBantime      int // seconds, first-offence ban
	RecidiveBantime int // seconds, repeat offenders (3 bans in 24h)
	MaxRetry        int // failed attempts within findtime before a ban
	IgnoreIPs       []string
}

// Fail2banJailScript renders the jail.local content.
func Fail2banJailScript(cfg Fail2banJailConfig) string {
	sshdBantime := cfg.SSHBantime
	if sshdBantime <= 0 {
		sshdBantime = 3600
	}
	recidiveBantime := cfg.RecidiveBantime
	if recidiveBantime <= 0 {
		recidiveBantime = 82800
	}
	maxRetry := cfg.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 5
	}
	ignore := "127.0.0.1/8 ::1"
	if len(cfg.IgnoreIPs) > 0 {
		ignore += " " + strings.Join(cfg.IgnoreIPs, " ")
	}
	return fmt.Sprintf(`[sshd]
enabled = true
port = 22
logpath = %%(sshd_log)s
backend = %%(sshd_backend)s
maxretry = %d
findtime = 600
bantime = %d
ignoreip = %s

# Repeat offenders: 3 bans within 24h get a longer all-ports ban.
[recidive]
enabled = true
logpath = /var/log/fail2ban.log
banaction = %%(banaction_allports)s
bantime = %d
findtime = 86400
maxretry = 3
`, maxRetry, sshdBantime, ignore, recidiveBantime)
}

// InstallFail2banJail writes jail.local only when the content differs and
// reloads fail2ban only in that case (idempotent, never disturbs a healthy
// config).
func InstallFail2banJail(client *goss.Client, cfg Fail2banJailConfig) error {
	content := Fail2banJailScript(cfg)
	out, _, err := ssh.Run(client, `sudo cat /etc/fail2ban/jail.local 2>/dev/null || true`)
	if err != nil {
		return fmt.Errorf("fail2ban jail read: %w", err)
	}
	if strings.TrimRight(out, "\r\n") == strings.TrimRight(content, "\r\n") {
		fmt.Println("fail2ban: jail.local up to date")
		return nil
	}
	script := fmt.Sprintf(`
cat > /tmp/jail.local << 'EOF'
%s
EOF
sudo cp /tmp/jail.local /etc/fail2ban/jail.local
sudo chmod 0644 /etc/fail2ban/jail.local
sudo fail2ban-client reload >/dev/null 2>&1 || sudo systemctl reload fail2ban 2>/dev/null || true
echo "fail2ban: jail.local updated and reloaded"
`, content)
	if _, _, err := ssh.Run(client, script); err != nil {
		return fmt.Errorf("fail2ban jail install: %w", err)
	}
	fmt.Println("fail2ban: jail.local updated and reloaded")
	return nil
}
