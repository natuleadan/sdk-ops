package docker

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

const dockerScript = `#!/bin/bash
set -euo pipefail

echo "=== sdk-ops: Install Docker ==="
if command -v docker &>/dev/null; then
    echo "Docker already installed, skipping"
    exit 0
fi

# The provider mirror (mirror.<provider>) can hang — switch to the official
# Ubuntu/Debian archives first (provider-agnostic), so the apt (docker-ce)
# resolves during the install.
sudo sed -i -E 's|https?://mirror[^/ ]*/ubuntu|http://archive.ubuntu.com/ubuntu|g' /etc/apt/sources.list.d/*.sources /etc/apt/sources.list 2>/dev/null || true
sudo sed -i -E 's|mirror\+file://[^ ]*debian-security[^ ]*|http://deb.debian.org/debian-security|g' /etc/apt/sources.list.d/*.sources 2>/dev/null || true
sudo sed -i -E 's|mirror\+file://[^ ]*debian[^ ]*|http://deb.debian.org/debian|g' /etc/apt/sources.list.d/*.sources 2>/dev/null || true
# The ForceIPv6 breaks v4-only hosts (the apt would try the unreachable v6):
# remove it ALWAYS, re-add only on v4-less hosts.
sudo rm -f /etc/apt/apt.conf.d/99force-ipv6 2>/dev/null || true
if ! ip -4 addr show | grep -q 'inet '; then
  echo 'Acquire::ForceIPv6 "true";' | sudo tee /etc/apt/apt.conf.d/99force-ipv6 >/dev/null 2>&1 || true
fi
sudo apt-get update >/dev/null 2>&1 || true

curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $(whoami) 2>/dev/null || true

echo "=== sdk-ops: Enable Docker service ==="
sudo systemctl enable docker
sudo systemctl start docker

docker --version
docker compose version 2>/dev/null || true
echo "=== sdk-ops: Docker installed ==="
`

func Install(client *goss.Client) error {
	fmt.Println("  → Installing Docker...")
	out, _, err := ssh.Run(client, dockerScript)
	if err != nil {
		return fmt.Errorf("docker install failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)
	return EnsureNetworking(client)
}

// EnsureNetworking loads the iptables NAT modules and restarts the daemon if
// the nat table lacks Docker chains. IPv6-only hosts do not auto-load
// iptable_nat, which breaks `docker run -p` (the DOCKER DNAT chain never gets
// created and publishes fail with "No chain/target/match by that name").
func EnsureNetworking(client *goss.Client) error {
	script := `
sudo modprobe iptable_nat ip6table_nat 2>/dev/null || true
if ! sudo nft list table ip nat 2>/dev/null | grep -q 'chain DOCKER'; then
  echo "  → docker networking: recreating nat chains (restarting docker)..."
  sudo systemctl restart docker
  sleep 3
fi
echo "docker networking: OK"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("docker networking: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func Check(client *goss.Client) (string, error) {
	checks := []string{
		"docker --version 2>/dev/null && echo 'docker: OK' || echo 'docker: MISSING'",
		"docker compose version 2>/dev/null && echo 'compose: OK' || echo 'compose: MISSING'",
		"systemctl is-active docker --quiet && echo 'docker-daemon: OK' || echo 'docker-daemon: MISSING'",
	}
	var cmd strings.Builder
	for _, c := range checks {
		cmd.WriteString(c + "; ")
	}
	out, _, err := ssh.Run(client, cmd.String())
	if err != nil {
		return "", fmt.Errorf("docker check: %w", err)
	}
	return out, nil
}
