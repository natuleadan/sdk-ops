package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type Config struct {
	User          string
	SSHPort       int // 0 = don't migrate, >0 = migrate SSH to this port (e.g. 2222)
	EnableMonitor bool
	LockRoot      bool // lock root password after creating sdkops user
	EnableAuditd  bool // install auditd
	EnableLynis   bool // install Lynis security auditor
	EnableUSG     bool // install Ubuntu Security Guide
}

func DefaultConfig() Config {
	return Config{
		User:          "sdkops",
		SSHPort:       0,
		EnableMonitor: false,
		LockRoot:      false,
		EnableAuditd:  false,
		EnableLynis:   false,
		EnableUSG:     false,
	}
}

func (c Config) MigrateSSH() bool {
	return c.SSHPort > 0 && c.SSHPort != 22
}

func Apply(client *goss.Client, cfg Config) error {
	steps := []struct {
		label string
		fn    func(*goss.Client, Config) error
	}{
		{"install_packages", installPackages},
		{"create_user", createUser},
		{"kernel_tuning", kernelTuning},
		{"remove_unused_services", removeUnusedServices},
		{"fail2ban", fail2banAndUpgrades},
		{"ssh_hardening", sshHardening},
		{"nftables", nftablesFirewall},
		{"auditd", installAuditd},
		{"lynis", installLynis},
		{"usg", installUSG},
		{"node_exporter", installNodeExporter},
	}

	for i, s := range steps {
		fmt.Printf("  Step %d/%d: %s\n", i+1, len(steps), s.label)
		if err := s.fn(client, cfg); err != nil {
			fmt.Printf("  ⚠️  Step %s had issues: %v\n", s.label, err)
			fmt.Printf("  → Continuing to next step...\n\n")
			continue
		}
		fmt.Printf("  ✓ %s done\n\n", s.label)
	}

	fmt.Println("  → Hardening complete!")
	return nil
}

func Check(client *goss.Client) (string, error) {
	checks := []string{
		"sudo systemctl is-active nftables --quiet && echo 'nftables: OK' || echo 'nftables: MISSING'",
		"sudo nft list table inet filter 2>/dev/null | grep -q 'tcp dport 6443' && echo 'nftables-6443: OK' || echo 'nftables-6443: MISSING'; sudo nft list table inet filter 2>/dev/null | grep -q 'tcp dport 22' && echo 'nftables-22: OK' || echo 'nftables-22: MISSING'",
		"sudo systemctl is-active fail2ban --quiet && echo 'fail2ban: OK' || echo 'fail2ban: MISSING'",
		"sudo systemctl is-active ssh 2>/dev/null || sudo systemctl is-active sshd --quiet && echo 'sshd: OK' || echo 'sshd: MISSING'",
		"sudo grep -q '^PasswordAuthentication no' /etc/ssh/sshd_config && echo 'pw-auth: OK' || echo 'pw-auth: FAIL'",
		"sudo grep -q '^PermitRootLogin no' /etc/ssh/sshd_config && echo 'root-login: OK' || echo 'root-login: FAIL'",
		"sudo grep -q '^MaxAuthTries 3' /etc/ssh/sshd_config && echo 'max-auth-tries: OK' || echo 'max-auth-tries: FAIL'",
		"sudo systemctl is-active auditd --quiet 2>/dev/null && echo 'auditd: OK' || echo 'auditd: MISSING'",
		"command -v lynis &>/dev/null && echo 'lynis: OK' || echo 'lynis: MISSING'",
		"command -v usg &>/dev/null && echo 'usg: OK' || echo 'usg: MISSING'",
	}
	var cmd strings.Builder
	for _, c := range checks {
		cmd.WriteString(c + "; ")
	}
	out, _, err := ssh.Run(client, cmd.String())
	if err != nil {
		return "", fmt.Errorf("check: %w", err)
	}
	return out, nil
}
