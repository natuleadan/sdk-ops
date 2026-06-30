package hardening

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func installPackages(client *goss.Client, cfg Config) error {
	fmt.Println("  → Installing packages (nftables, fail2ban, htop)...")

	script := `
for i in $(seq 1 30); do
    if ! fuser /var/lib/dpkg/lock-frontend /var/lib/apt/lists/lock 2>/dev/null; then
        break
    fi
    sleep 3
done
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nftables fail2ban unattended-upgrades htop iotop net-tools 2>&1
`
	return ssh.RunStream(client, script)
}

func createUser(client *goss.Client, cfg Config) error {
	script := fmt.Sprintf(`
if ! id "%s" &>/dev/null; then
    useradd -m -s /bin/bash -G sudo "%s"
    echo "%s ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/%[1]s
fi
`, cfg.User, cfg.User, cfg.User)

	if cfg.LockRoot {
		script += `passwd -l root 2>/dev/null || true
`
	}

	fmt.Printf("  → Creating user %s...\n", cfg.User)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("create user: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func kernelTuning(client *goss.Client, cfg Config) error {
	fmt.Println("  → Kernel tuning (sysctl)...")
	script := `
cat >> /etc/sysctl.conf << 'SYSCTL'
net.ipv4.tcp_syncookies=1
net.ipv4.conf.all.rp_filter=1
net.ipv4.conf.default.rp_filter=1
net.ipv4.conf.all.accept_source_route=0
net.ipv6.conf.all.accept_source_route=0
net.ipv4.conf.all.accept_redirects=0
net.ipv6.conf.all.accept_redirects=0
net.ipv4.conf.all.send_redirects=0
net.ipv4.conf.default.send_redirects=0
kernel.yama.ptrace_scope=1
SYSCTL
sysctl -p
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("kernel tuning: %w", err)
	}
	fmt.Print(out)
	return nil
}

func fail2banAndUpgrades(client *goss.Client, cfg Config) error {
	sshPort := 22
	if cfg.MigrateSSH() {
		sshPort = cfg.SSHPort
	}

	script := fmt.Sprintf(`
cat > /etc/fail2ban/jail.local << 'F2B'
[DEFAULT]
bantime = 3600
findtime = 600
maxretry = 5
[sshd]
enabled = true
port = %d
logpath = %%(sshd_log)s
backend = %%(sshd_backend)s
F2B
systemctl restart fail2ban
echo "fail2ban: OK"

cat > /etc/apt/apt.conf.d/20auto-upgrades << 'UP'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Download-Upgradeable-Packages "1";
APT::Periodic::AutocleanInterval "7";
APT::Periodic::Unattended-Upgrade "1";
UP
echo "unattended-upgrades: OK"
`, sshPort)

	fmt.Println("  → fail2ban + unattended-upgrades...")
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("fail2ban: %w", err)
	}
	fmt.Print(out)
	return nil
}

func sshHardening(client *goss.Client, cfg Config) error {
	script := fmt.Sprintf(`
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak 2>/dev/null

# Harden SSH (CIS Level 1)
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config
sed -i 's/^#PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^#MaxAuthTries.*/MaxAuthTries 3/' /etc/ssh/sshd_config
grep -q '^MaxAuthTries' /etc/ssh/sshd_config || echo 'MaxAuthTries 3' >> /etc/ssh/sshd_config
sed -i 's/^#PermitEmptyPasswords.*/PermitEmptyPasswords no/' /etc/ssh/sshd_config
grep -q '^PermitEmptyPasswords' /etc/ssh/sshd_config || echo 'PermitEmptyPasswords no' >> /etc/ssh/sshd_config

# Copy SSH key to new user
mkdir -p /home/%s/.ssh
cp /root/.ssh/authorized_keys /home/%s/.ssh/ 2>/dev/null || true
chown -R %s:%s /home/%s/.ssh
`, cfg.User, cfg.User, cfg.User, cfg.User, cfg.User)

	if cfg.MigrateSSH() {
		script += fmt.Sprintf(`
# Add new SSH port %d while keeping port 22
echo "Port %d" >> /etc/ssh/sshd_config
echo "SSH port %d added (port 22 kept)"
`, cfg.SSHPort, cfg.SSHPort, cfg.SSHPort)
	} else {
		script += `
echo "SSH port 22 (unchanged)"
`
	}

	script += `
# Restart SSH
if systemctl list-units --type=service 2>/dev/null | grep -q sshd.service; then
    systemctl restart sshd
else
    systemctl restart ssh
fi
`

	fmt.Printf("  → SSH hardening (keep port 22, no password auth)...\n")
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	fmt.Print(out)
	return nil
}

func removeUnusedServices(client *goss.Client, cfg Config) error {
	fmt.Println("  → Removing unused services (telnet, ftp, rsh, rpcbind, avahi, cups)...")
	script := `
for svc in telnet ftp rsh rpcbind avahi-daemon cups; do
    systemctl disable --now "$svc" 2>/dev/null || true
done
for pkg in telnet ftp rsh-client rpcbind avahi-daemon cups; do
    apt-get remove -y -qq "$pkg" 2>/dev/null || true
done
echo "unused-services: OK"
`
	return ssh.RunStream(client, script)
}

func nftablesFirewall(client *goss.Client, cfg Config) error {
	openPorts := "22, 80, 443, 6443"
	if cfg.MigrateSSH() {
		openPorts = fmt.Sprintf("22, %d, 80, 443, 6443", cfg.SSHPort)
	}

	script := fmt.Sprintf(`
cat > /etc/nftables.conf << 'NFT'
#!/usr/sbin/nft -f
flush ruleset
table inet filter {
    chain input {
        type filter hook input priority 0; policy drop;
        iif lo accept
        ct state established,related accept
        tcp dport { %s } accept
        ip protocol icmp accept
        ip6 nexthdr icmpv6 accept
    }
    chain forward { type filter hook forward priority 0; policy accept; }
    chain output { type filter hook output priority 0; policy accept; }
}
NFT
systemctl enable nftables && systemctl restart nftables && echo "nftables: OK (ports %s)"
`, openPorts, openPorts)

	fmt.Println("  → nftables (locking down ports)...")
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("nftables: %w", err)
	}
	fmt.Print(out)
	return nil
}

func installAuditd(client *goss.Client, cfg Config) error {
	if !cfg.EnableAuditd {
		fmt.Println("  → Skipping auditd (not enabled)")
		return nil
	}
	fmt.Println("  → Installing auditd...")
	script := `
if ! command -v auditd &>/dev/null; then
    apt-get install -y -qq auditd audispd-plugins 2>&1 | tail -1
fi
systemctl enable auditd 2>/dev/null || true
systemctl start auditd 2>/dev/null || true
echo "auditd: OK"
`
	return ssh.RunStream(client, script)
}

func installLynis(client *goss.Client, cfg Config) error {
	if !cfg.EnableLynis {
		fmt.Println("  → Skipping Lynis (not enabled)")
		return nil
	}
	fmt.Println("  → Installing Lynis security auditor...")
	script := `
if ! command -v lynis &>/dev/null; then
    apt-get install -y -qq lynis 2>&1 | tail -1
fi
echo "lynis: OK"
`
	return ssh.RunStream(client, script)
}

func installNodeExporter(client *goss.Client, cfg Config) error {
	if !cfg.EnableMonitor {
		fmt.Println("  → Skipping node_exporter (not enabled)")
		return nil
	}

	fmt.Println("  → Installing node_exporter...")
	script := `
NODE_EXPORTER_VER="1.8.2"
if command -v node_exporter &>/dev/null; then
    echo "node_exporter already installed"
    exit 0
fi
cd /tmp
curl -fsSLO "https://github.com/prometheus/node_exporter/releases/download/v${NODE_EXPORTER_VER}/node_exporter-${NODE_EXPORTER_VER}.linux-amd64.tar.gz"
tar xzf "node_exporter-${NODE_EXPORTER_VER}.linux-amd64.tar.gz"
cp "node_exporter-${NODE_EXPORTER_VER}.linux-amd64/node_exporter" /usr/local/bin/
rm -rf "node_exporter-${NODE_EXPORTER_VER}.linux-amd64" "node_exporter-${NODE_EXPORTER_VER}.linux-amd64.tar.gz"

cat > /etc/systemd/system/node_exporter.service << 'SERVICE'
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

systemctl daemon-reload
systemctl enable node_exporter
systemctl start node_exporter
echo "node_exporter: OK (port 9100)"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("node_exporter: %w", err)
	}
	fmt.Print(out)
	return nil
}
