package main

import (
	"fmt"
	"strings"

	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/docker"
	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
)

// precheckNode verifies and enforces the security prerequisites before an
// operation (db create, port exposure): nftables active, provider allowlist
// installed (auto-installs cf-normal with the operator IP when missing), and
// Docker when needed. Security by default: no operation can leave a port
// exposed without the allowlist base.
func precheckNode(conn *golang_ssh.Client, node string, f *infraFlags, needDocker bool) error {
	// 1. nftables active
	if out, _, _ := ssh.Run(conn, `systemctl is-active nftables`); strings.TrimSpace(out) != "active" {
		fmt.Println("  → precheck: enabling nftables service...")
		if _, _, err := ssh.Run(conn, `sudo systemctl enable --now nftables 2>/dev/null || true`); err != nil {
			return fmt.Errorf("precheck nftables: %w", err)
		}
	}

	// 2. Provider allowlist installed (sets admin4/allow4 present)
	if out, _, _ := ssh.Run(conn, `sudo nft list table inet filter 2>/dev/null | grep -q 'set admin4' && echo yes || echo no`); strings.TrimSpace(out) != "yes" {
		fmt.Println("  → precheck: allowlist not installed — installing cf-normal with your IP...")
		if err := installAllowlistOnNode(conn, node, f, hardening.AllowlistNormal, "cf"); err != nil {
			return fmt.Errorf("precheck allowlist: %w", err)
		}
	}

	// 3. Docker (db / deploy flows)
	if needDocker {
		if out, _, _ := ssh.Run(conn, `command -v docker >/dev/null 2>&1 && echo yes || echo no`); strings.TrimSpace(out) != "yes" {
			fmt.Println("  → precheck: installing Docker...")
			if err := docker.Install(conn); err != nil {
				return fmt.Errorf("precheck docker: %w", err)
			}
		}
	}

	fmt.Println("  → precheck: OK")
	return nil
}
