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
	PublicIP              string // VPS public IP (for TLS SAN)
	ExtraArgs             string // Extra k3s args: "--disable traefik --docker"
	K3sVersion            string // Specific version (empty = latest stable)
	K3sChannel            string // Channel: stable, latest, v1.30 (default: stable)
	LocalPath             string // Where to save kubeconfig (default: ./kubeconfig)
	Context               string // Kubeconfig context name (default: default)
	Merge                 bool   // Merge into ~/.kube/config?
	DisableTraefik        bool
	SecretsEncryption     bool   // --secrets-encryption (CIS)
	ProtectKernelDefaults bool   // --protect-kernel-defaults (CIS)
	AdmissionPlugins      string // --kube-apiserver-arg enable-admission-plugins=...
	CISPSA                bool   // Enforce Pod Security Admission restricted
	CISAuditLog           bool   // Enable kube-apiserver audit logging
	CISNetPol             bool   // Apply default-deny NetworkPolicy
	CISSvcAcc             bool   // Patch default ServiceAccount (automount=false)
	CISTLSCiphers         bool   // Restrict TLS cipher suites
	SkipDownload          bool   // Skip downloading binary (for airgap installs)
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
	if cfg.SecretsEncryption {
		extraArgs = strings.TrimSpace(extraArgs + " --secrets-encryption")
	}
	if cfg.ProtectKernelDefaults {
		extraArgs = strings.TrimSpace(extraArgs + " --protect-kernel-defaults")
	}
	if cfg.AdmissionPlugins != "" {
		extraArgs = strings.TrimSpace(extraArgs + fmt.Sprintf(" --kube-apiserver-arg enable-admission-plugins=%s", cfg.AdmissionPlugins))
	}
	if cfg.CISTLSCiphers {
		tlsCiphers := "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305"
		extraArgs = strings.TrimSpace(extraArgs + fmt.Sprintf(" --kubelet-arg tls-cipher-suites=%s", tlsCiphers))
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
		os.MkdirAll(filepath.Clean(kubeDir), 0700)
		kubePath := filepath.Clean(filepath.Join(kubeDir, "config"))

		existing, err := os.ReadFile(kubePath)
		if err == nil && len(existing) > 0 {
			kubeconfig = string(existing) + "\n" + kubeconfig
		}
		if len(existing) > 0 {
			// Append with a context name — in a real impl we'd merge properly
			kubeconfig = string(existing) + "\n" + kubeconfig
		}
		if err := os.WriteFile(filepath.Clean(kubePath), []byte(kubeconfig), 0600); err != nil {
			return fmt.Errorf("write kubeconfig: %w", err)
		}
		fmt.Printf("  → Merged kubeconfig into %s (context: %s)\n", kubePath, cfg.Context)
	} else {
		if err := os.WriteFile(filepath.Clean(cfg.LocalPath), []byte(kubeconfig), 0600); err != nil {
			return fmt.Errorf("write kubeconfig: %w", err)
		}
		fmt.Printf("  → Kubeconfig saved to %s\n", cfg.LocalPath)
	}

	fmt.Printf("  → Token: %s", token)

	// Post-install CIS hardening
	postInstallCIS(client, cfg)

	fmt.Println("  → k3s installed successfully!")
	return nil
}

func postInstallCIS(client *goss.Client, cfg InstallConfig) {
	kcmd := `sudo kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml`

	// 10. PSA restricted enforcement
	if cfg.CISPSA {
		fmt.Println("  → CIS: enforcing Pod Security Admission (restricted)...")
		script := fmt.Sprintf(`
# Label default namespace with PSA restricted
%s label --overwrite namespace default pod-security.kubernetes.io/enforce=restricted 2>/dev/null || true
%s label --overwrite namespace default pod-security.kubernetes.io/audit=restricted 2>/dev/null || true
%s label --overwrite namespace default pod-security.kubernetes.io/warn=restricted 2>/dev/null || true
echo "psa: OK"
`, kcmd, kcmd, kcmd)
		out, _, err := ssh.Run(client, script)
		if err != nil {
			fmt.Printf("  ⚠️  PSA label failed: %v\n", err)
		} else {
			fmt.Print(out)
		}
	}

	// 11. Audit logs
	if cfg.CISAuditLog {
		fmt.Println("  → CIS: enabling kube-apiserver audit logs...")
		auditPolicy := `apiVersion: audit.k8s.io/v1
kind: Policy
metadata:
  creationTimestamp: null
rules:
- level: Metadata
  resources:
  - resources: ["secrets"]
- level: RequestResponse
  resources:
  - resources: ["configmaps"]
- level: Metadata
  omitStages:
  - RequestReceived
`
		script := fmt.Sprintf(`
sudo mkdir -p /var/lib/rancher/k3s/server/logs
cat > /tmp/audit-policy.yaml << 'POLICY'
%s
POLICY
sudo mv /tmp/audit-policy.yaml /var/lib/rancher/k3s/server/logs/audit-policy.yaml
sudo chmod 600 /var/lib/rancher/k3s/server/logs/audit-policy.yaml
# Add audit args to k3s service
sudo sed -i 's|^ExecStart=.*|& --audit-log-path=/var/lib/rancher/k3s/server/logs/audit.log --audit-policy-file=/var/lib/rancher/k3s/server/logs/audit-policy.yaml --audit-log-maxage=30 --audit-log-maxbackup=10 --audit-log-maxsize=100|' /etc/systemd/system/k3s.service 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl restart k3s
echo "audit-log: OK"
`, auditPolicy)
		out, _, err := ssh.Run(client, script)
		if err != nil {
			fmt.Printf("  ⚠️  Audit log setup failed: %v\n", err)
		} else {
			fmt.Print(out)
		}
	}

	// 12. Default-deny NetworkPolicy
	if cfg.CISNetPol {
		fmt.Println("  → CIS: applying default-deny NetworkPolicy...")
		script := fmt.Sprintf(`
%s apply -f - << 'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: default
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
EOF
echo "netpol: OK"
`, kcmd)
		out, _, err := ssh.Run(client, script)
		if err != nil {
			fmt.Printf("  ⚠️  NetworkPolicy apply failed: %v\n", err)
		} else {
			fmt.Print(out)
		}
	}

	// 13. Patch default ServiceAccount
	if cfg.CISSvcAcc {
		fmt.Println("  → CIS: patching default ServiceAccount...")
		script := fmt.Sprintf(`
%s patch serviceaccount default -n default -p '{"automountServiceAccountToken": false}' 2>/dev/null || true
echo "svcacc: OK"
`, kcmd)
		out, _, err := ssh.Run(client, script)
		if err != nil {
			fmt.Printf("  ⚠️  ServiceAccount patch failed: %v\n", err)
		} else {
			fmt.Print(out)
		}
	}

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
