package cloudinit

import (
	"fmt"
	"strings"
)

type Config struct {
	User           string
	SSHPort        int
	Mode           string // k3s, docker, bare
	CrowdSec       bool
	EnableMonitor  bool
	DisableTraefik bool
	SSHKeys        []string
}

func DefaultConfig() Config {
	return Config{
		User:    "sdkops",
		SSHPort: 2222,
		Mode:    "k3s",
	}
}

func Generate(cfg Config) string {
	user := cfg.User
	sshPort := cfg.SSHPort

	script := fmt.Sprintf(`#cloud-config
package_update: true
package_upgrade: false

packages:
  - nftables
  - fail2ban
  - unattended-upgrades
  - htop
  - iotop
  - curl

users:
  - name: %s
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
`, user)

	// SSH keys
	if len(cfg.SSHKeys) > 0 {
		script += "    ssh_authorized_keys:\n"
		for _, key := range cfg.SSHKeys {
			script += fmt.Sprintf("      - %s\n", key)
		}
	}

	script += fmt.Sprintf(`
write_files:
  - path: /etc/sysctl.d/99-sdk-ops.conf
    content: |
      net.ipv4.tcp_syncookies=1
      net.ipv4.conf.all.rp_filter=1
      net.ipv4.conf.default.rp_filter=1
      net.ipv4.conf.all.accept_source_route=0
      net.ipv6.conf.all.accept_source_route=0
      net.ipv4.conf.all.accept_redirects=0
      net.ipv6.conf.all.accept_redirects=0
      kernel.yama.ptrace_scope=1

  - path: /etc/fail2ban/jail.local
    content: |
      [DEFAULT]
      bantime = 3600
      findtime = 600
      maxretry = 5
      [sshd]
      enabled = true
      port = %d
      logpath = %%(sshd_log)s
      backend = %%(sshd_backend)s

  - path: /etc/apt/apt.conf.d/20auto-upgrades
    content: |
      APT::Periodic::Update-Package-Lists "1";
      APT::Periodic::Download-Upgradeable-Packages "1";
      APT::Periodic::AutocleanInterval "7";
      APT::Periodic::Unattended-Upgrade "1";
`, sshPort)

	// SSH hardening
	script += fmt.Sprintf(`
  - path: /etc/ssh/sshd_config.d/50-sdk-ops.conf
    content: |
      Port %d
      PasswordAuthentication no
      PermitRootLogin prohibit-password
`, sshPort)

	// nftables
	script += fmt.Sprintf(`
  - path: /etc/nftables.conf
    content: |
      #!/usr/sbin/nft -f
      flush ruleset
      table inet filter {
          chain input {
              type filter hook input priority 0; policy drop;
              iif lo accept
              ct state established,related accept
              tcp dport { %d, 80, 443 } accept
              tcp dport 6443 accept
              ip protocol icmp accept
              ip6 nexthdr icmpv6 accept
          }
          chain forward { type filter hook forward priority 0; policy drop; }
          chain output { type filter hook output priority 0; policy accept; }
      }
`, sshPort)

	// Lock root
	script += `
  - path: /etc/sudoers.d/90-cloud-init-users
    content: |
      # User rules for sdk-ops
runcmd:
  - [ passwd, -l, root ]
  - [ sysctl, -p ]
  - [ systemctl, enable, nftables ]
  - [ systemctl, restart, nftables ]
  - [ systemctl, enable, fail2ban ]
  - [ systemctl, restart, fail2ban ]
  - [ systemctl, enable, unattended-upgrades ]
  - [ systemctl, restart, unattended-upgrades ]
`

	// Docker
	if cfg.Mode == "docker" || cfg.Mode == "k3s" {
		script += `  - [ curl, -fsSL, https://get.docker.com, -o, /tmp/get-docker.sh ]
  - [ sh, /tmp/get-docker.sh ]
  - [ systemctl, enable, docker ]
  - [ systemctl, start, docker ]
`
	}

	// k3s
	if cfg.Mode == "k3s" {
		traefikFlag := ""
		if cfg.DisableTraefik {
			traefikFlag = "--disable traefik"
		}
		script += fmt.Sprintf(`  - [ sh, -c, 'curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="server --tls-san $(curl -s http://169.254.169.254/latest/meta-data/public-ipv4 2>/dev/null || hostname -I | awk "{print \$1}") %s" sh -' ]
`, traefikFlag)
		script += `  - [ sh, -c, 'mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs && echo "sdk-ops-init" > /opt/sdk-ops/.version' ]
`
	}

	// CrowdSec
	if cfg.CrowdSec {
		script += `  - [ curl, -fsSL, https://install.crowdsec.net, -o, /tmp/install-crowdsec.sh ]
  - [ sh, /tmp/install-crowdsec.sh ]
  - [ systemctl, enable, crowdsec ]
  - [ systemctl, start, crowdsec ]
`
	}

	// Node Exporter
	if cfg.EnableMonitor {
		script += `  - [ sh, -c, 'VER="1.8.2"; curl -fsSLO "https://github.com/prometheus/node_exporter/releases/download/v${VER}/node_exporter-${VER}.linux-amd64.tar.gz" && tar xzf "node_exporter-${VER}.linux-amd64.tar.gz" && cp "node_exporter-${VER}.linux-amd64/node_exporter" /usr/local/bin/ && rm -rf "node_exporter-${VER}.linux-amd64" "node_exporter-${VER}.linux-amd64.tar.gz"' ]
`
		script += `  - [ sh, -c, 'cat > /etc/systemd/system/node_exporter.service << "SERVICE"
[Unit]
Description=Prometheus Node Exporter
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/node_exporter --web.listen-address=:9100
Restart=always

[Install]
WantedBy=multi-user.target
SERVICE
systemctl daemon-reload && systemctl enable node_exporter && systemctl start node_exporter' ]
`
	}

	// Cleanup old SSH port 22
	script += fmt.Sprintf(`  - [ sed, -i, '/^Port 22$/d', /etc/ssh/sshd_config ]
  - [ systemctl, restart, ssh ]
  - [ rm, -f, /root/.ssh/authorized_keys ]
  - [ echo, "sdk-ops cloud-init provisioning complete" ]
`)

	return strings.TrimSpace(script)
}

func ValidateMode(mode string) error {
	switch mode {
	case "k3s", "docker", "bare":
		return nil
	default:
		return fmt.Errorf("invalid mode %q: must be k3s, docker, or bare", mode)
	}
}
