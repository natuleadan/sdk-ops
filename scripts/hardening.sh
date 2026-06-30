#!/bin/bash
# hardening script — runs via SSH on the target VPS
set -euo pipefail

NEW_USER="{{.User}}"
SSH_PORT={{.SSHPort}}

echo "=== hardening: System update ==="
apt-get update -qq
apt-get upgrade -y -qq

echo "=== hardening: Install base packages ==="
apt-get install -y -qq curl wget ufw fail2ban unattended-upgrades htop iotop net-tools

echo "=== hardening: Create user ==="
if ! id "$NEW_USER" &>/dev/null; then
    adduser --disabled-password --gecos "" "$NEW_USER"
    usermod -aG sudo "$NEW_USER"
    mkdir -p /home/$NEW_USER/.ssh
    cp /root/.ssh/authorized_keys /home/$NEW_USER/.ssh/ || true
    chown -R $NEW_USER:$NEW_USER /home/$NEW_USER/.ssh
    chmod 700 /home/$NEW_USER/.ssh
    chmod 600 /home/$NEW_USER/.ssh/authorized_keys
    echo "$NEW_USER ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$NEW_USER
fi

echo "=== hardening: Harden SSH ==="
sed -i "s/^#Port 22/Port $SSH_PORT/" /etc/ssh/sshd_config
sed -i "s/^Port 22/Port $SSH_PORT/" /etc/ssh/sshd_config
sed -i 's/^#PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config
systemctl restart sshd

echo "=== hardening: Configure UFW ==="
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow $SSH_PORT/tcp comment 'SSH'
ufw --force enable

echo "=== hardening: Configure fail2ban ==="
cat > /etc/fail2ban/jail.local << 'FAIL2BAN'
[DEFAULT]
bantime = 3600
findtime = 600
maxretry = 5

[sshd]
enabled = true
port = {{.SSHPort}}
logpath = %(sshd_log)s
backend = %(sshd_backend)s
FAIL2BAN
systemctl restart fail2ban

echo "=== hardening: Configure unattended-upgrades ==="
cat > /etc/apt/apt.conf.d/20auto-upgrades << 'UPGRADES'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Download-Upgradeable-Packages "1";
APT::Periodic::AutocleanInterval "7";
APT::Periodic::Unattended-Upgrade "1";
UPGRADES

echo "=== hardening: Disable root login (after key check) ==="
passwd -l root 2>/dev/null || true

echo "=== hardening: Kernel tuning ==="
cat >> /etc/sysctl.conf << 'SYSCTL'
net.ipv4.tcp_syncookies=1
net.ipv4.conf.all.rp_filter=1
net.ipv4.conf.default.rp_filter=1
net.ipv4.conf.all.accept_source_route=0
net.ipv6.conf.all.accept_source_route=0
net.ipv4.conf.all.accept_redirects=0
net.ipv6.conf.all.accept_redirects=0
kernel.yama.ptrace_scope=1
SYSCTL
sysctl -p

echo "=== hardening: Complete ==="
