package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// Fail2banBan bans an explicit IP (or CIDR) via the node's fail2ban sshd
// jail. IPs are always literal — no auto-detection anywhere.
func Fail2banBan(client *goss.Client, ip string) error {
	ip = strings.TrimSpace(ip)
	if _, err := ValidateCIDR(ip); err != nil {
		return fmt.Errorf("ban requires a literal IP/CIDR: %w", err)
	}
	script := fmt.Sprintf(`
if ! command -v fail2ban-client >/dev/null 2>&1; then
  echo "fail2ban not installed (run infra init first)" >&2
  exit 1
fi
sudo fail2ban-client set sshd banip %[1]s 2>&1 | tail -1
echo "banned %[1]s"
`, ip)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("fail2ban ban %s: %w\n%s", ip, err, out)
	}
	fmt.Print(out)
	return nil
}

// Fail2banUnban unbans an explicit IP via the node's fail2ban sshd jail.
func Fail2banUnban(client *goss.Client, ip string) error {
	ip = strings.TrimSpace(ip)
	if _, err := ValidateCIDR(ip); err != nil {
		return fmt.Errorf("unban requires a literal IP/CIDR: %w", err)
	}
	script := fmt.Sprintf(`
if ! command -v fail2ban-client >/dev/null 2>&1; then
  echo "fail2ban not installed (run infra init first)" >&2
  exit 1
fi
sudo fail2ban-client unban %[1]s 2>&1 | tail -1
echo "unbanned %[1]s"
`, ip)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("fail2ban unban %s: %w\n%s", ip, err, out)
	}
	fmt.Print(out)
	return nil
}

// Fail2banBans lists the currently banned IPs on the node.
func Fail2banBans(client *goss.Client) (string, error) {
	out, _, err := ssh.Run(client, `sudo fail2ban-client status sshd 2>/dev/null | grep "Banned IP" || echo "no bans"`)
	if err != nil {
		return "", fmt.Errorf("fail2ban status: %w", err)
	}
	return out, nil
}
