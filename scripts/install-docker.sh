#!/bin/bash
set -euo pipefail

echo "=== docker: Install Docker ==="
if command -v docker &>/dev/null; then
    echo "Docker already installed, skipping"
    exit 0
fi

curl -fsSL https://get.docker.com | sh
usermod -aG docker $SUDO_USER || true

echo "=== docker: Enable Docker service ==="
systemctl enable docker
systemctl start docker

# Verify
docker --version
docker compose version
echo "=== docker: Installed ==="
