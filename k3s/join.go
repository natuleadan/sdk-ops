package k3s

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type JoinConfig struct {
	ServerIP    string
	Token       string
	ServerUser  string // SSH user for the server (if different from agent)
	ExtraArgs   string
	K3sVersion  string
	K3sChannel  string
}

func Join(agentClient, serverClient *goss.Client, cfg JoinConfig) error {
	fmt.Printf("  → Joining agent to server %s...\n", cfg.ServerIP)

	// Get token from server if not provided
	token := cfg.Token
	if token == "" && serverClient != nil {
		out, _, err := ssh.Run(serverClient, "cat /var/lib/rancher/k3s/server/token")
		if err != nil {
			return fmt.Errorf("fetch token from server: %w", err)
		}
		token = strings.TrimSpace(out)
	}
	if token == "" {
		return fmt.Errorf("token is required (provide --token or ensure SSH access to server)")
	}

	installCmd := "curl -sfL https://get.k3s.io"
	if cfg.K3sChannel != "" {
		installCmd = fmt.Sprintf("INSTALL_K3S_CHANNEL=%s %s", cfg.K3sChannel, installCmd)
	}
	if cfg.K3sVersion != "" {
		installCmd = fmt.Sprintf("INSTALL_K3S_VERSION=%s %s", cfg.K3sVersion, installCmd)
	}

	agentArgs := cfg.ExtraArgs
	installCmd = fmt.Sprintf("%s K3S_URL=https://%s:6443 K3S_TOKEN=%s INSTALL_K3S_EXEC='agent %s' sh -",
		installCmd, cfg.ServerIP, token, agentArgs)

	out, _, err := ssh.Run(agentClient, installCmd)
	if err != nil {
		return fmt.Errorf("join failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)

	fmt.Println("  → Agent joined successfully!")
	return nil
}
