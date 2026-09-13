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
	fmt.Printf("  -> Joining agent to server %s...\n", cfg.ServerIP)

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

	// Env vars must reach the install script (assignments before the pipe
	// only apply to curl) and the whole thing needs root — the caller may be
	// the post-hardening user (sdkops) with NOPASSWD sudo.
	env := fmt.Sprintf("K3S_URL=https://%s:6443 K3S_TOKEN=%s", cfg.ServerIP, token)
	if cfg.K3sChannel != "" {
		env = fmt.Sprintf("INSTALL_K3S_CHANNEL=%s %s", cfg.K3sChannel, env)
	}
	if cfg.K3sVersion != "" {
		env = fmt.Sprintf("INSTALL_K3S_VERSION=%s %s", cfg.K3sVersion, env)
	}
	installCmd := fmt.Sprintf(`SUDO=""; [ "$(id -u)" != "0" ] && SUDO="sudo"; curl -sfL https://get.k3s.io | $SUDO env %s INSTALL_K3S_EXEC="agent %s" sh -`, env, cfg.ExtraArgs)

	out, _, err := ssh.Run(agentClient, installCmd)
	if err != nil {
		return fmt.Errorf("join failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)

	fmt.Println("  -> Agent joined successfully!")
	return nil
}
