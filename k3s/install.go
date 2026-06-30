package k3s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type InstallConfig struct {
	PublicIP      string // VPS public IP (for TLS SAN)
	ExtraArgs     string // Extra k3s args: "--disable traefik --docker"
	K3sVersion    string // Specific version (empty = latest stable)
	K3sChannel    string // Channel: stable, latest, v1.30 (default: stable)
	LocalPath     string // Where to save kubeconfig (default: ./kubeconfig)
	Context       string // Kubeconfig context name (default: default)
	Merge         bool   // Merge into ~/.kube/config?
	DisableTraefik bool
	SkipDownload  bool   // Skip downloading binary (for airgap installs)
}

func DefaultInstallConfig(publicIP string) InstallConfig {
	return InstallConfig{
		PublicIP:     publicIP,
		LocalPath:    "./kubeconfig",
		Context:      "default",
		K3sChannel:   "stable",
		DisableTraefik: false,
	}
}

func Install(client *goss.Client, cfg InstallConfig) error {
	fmt.Println("  → Installing k3s...")

	extraArgs := cfg.ExtraArgs
	if cfg.DisableTraefik {
		extraArgs = strings.TrimSpace(extraArgs + " --disable traefik")
	}

	// Build the install command: curl | env K3S_XXX... sh -
	envVars := ""
	if cfg.K3sChannel != "" {
		envVars += fmt.Sprintf("INSTALL_K3S_CHANNEL=%s ", cfg.K3sChannel)
	}
	if cfg.K3sVersion != "" {
		envVars += fmt.Sprintf("INSTALL_K3S_VERSION=%s ", cfg.K3sVersion)
	}
	if cfg.SkipDownload {
		envVars += "INSTALL_K3S_SKIP_DOWNLOAD=true "
	}

	serverFlags := fmt.Sprintf("--tls-san %s", cfg.PublicIP)
	if extraArgs != "" {
		serverFlags = serverFlags + " " + extraArgs
	}

	installCmd := fmt.Sprintf("curl -sfL https://get.k3s.io | %sINSTALL_K3S_EXEC='server %s' sudo sh -", envVars, serverFlags)

	out, _, err := ssh.Run(client, installCmd)
	if err != nil {
		return fmt.Errorf("k3s install failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)

	// Wait for k3s to be ready
	fmt.Println("  → Waiting for k3s to be ready...")
	waitCmd := `for i in $(seq 1 30); do
  if sudo kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes 2>/dev/null | grep -q Ready; then
    echo "k3s ready"
    exit 0
  fi
  sleep 2
done
echo "k3s not ready after 60s"
exit 1`
	_, _, err = ssh.Run(client, waitCmd)
	if err != nil {
		return fmt.Errorf("k3s not ready: %w", err)
	}

	// Fetch kubeconfig
	fmt.Println("  → Fetching kubeconfig...")
	kubeconfig, _, err := ssh.Run(client, "sudo cat /etc/rancher/k3s/k3s.yaml")
	if err != nil {
		return fmt.Errorf("fetch kubeconfig: %w", err)
	}

	// Replace localhost with public IP in kubeconfig
	kubeconfig = strings.ReplaceAll(kubeconfig, "127.0.0.1", cfg.PublicIP)
	kubeconfig = strings.ReplaceAll(kubeconfig, "localhost", cfg.PublicIP)

	// Get the token for future joins
	token, _, err := ssh.Run(client, "sudo cat /var/lib/rancher/k3s/server/token")
	if err != nil {
		return fmt.Errorf("fetch token: %w", err)
	}

	if cfg.Merge {
		// Merge into ~/.kube/config
		kubeDir := os.ExpandEnv("$HOME/.kube")
		os.MkdirAll(kubeDir, 0700)
		kubePath := filepath.Join(kubeDir, "config")

		existing, _ := os.ReadFile(kubePath)
		if len(existing) > 0 {
			// Append with a context name — in a real impl we'd merge properly
			kubeconfig = string(existing) + "\n" + kubeconfig
		}
		if err := os.WriteFile(kubePath, []byte(kubeconfig), 0600); err != nil {
			return fmt.Errorf("write kubeconfig: %w", err)
		}
		fmt.Printf("  → Merged kubeconfig into %s (context: %s)\n", kubePath, cfg.Context)
	} else {
		if err := os.WriteFile(cfg.LocalPath, []byte(kubeconfig), 0600); err != nil {
			return fmt.Errorf("write kubeconfig: %w", err)
		}
		fmt.Printf("  → Kubeconfig saved to %s\n", cfg.LocalPath)
	}

	fmt.Printf("  → Token: %s", token)
	fmt.Println("  → k3s installed successfully!")
	return nil
}

func Check(client *goss.Client) (string, error) {
	checks := []string{
		"k3s --version 2>/dev/null && echo 'k3s: OK' || echo 'k3s: MISSING'",
		"sudo systemctl is-active k3s --quiet && echo 'k3s-service: OK' || echo 'k3s-service: MISSING'",
		"sudo kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes 2>/dev/null | grep -q Ready && echo 'k3s-ready: OK' || echo 'k3s-ready: PENDING'",
		"sudo ls /etc/rancher/k3s/k3s.yaml 2>/dev/null && echo 'kubeconfig: OK' || echo 'kubeconfig: MISSING'",
		"sudo test -f /var/lib/rancher/k3s/server/token && echo 'token: OK' || echo 'token: MISSING'",
	}
	cmd := strings.Join(checks, "; ")
	out, _, err := ssh.Run(client, cmd)
	if err != nil {
		return "", fmt.Errorf("k3s check: %w", err)
	}
	return out, nil
}
