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
