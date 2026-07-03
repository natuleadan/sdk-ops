package k3s

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type InstallConfig struct {
	PublicIP              string
	ExtraArgs             string
	K3sVersion            string
	K3sChannel            string
	LocalPath             string
	Context               string
	Merge                 bool
	DisableTraefik        bool
	SecretsEncryption     bool
	ProtectKernelDefaults bool
	AdmissionPlugins      string
	CISPSA                bool
	CISAuditLog           bool
	CISNetPol             bool
	CISSvcAcc             bool
	CISTLSCiphers         bool
	SkipDownload          bool
}

func DefaultInstallConfig(publicIP string) InstallConfig {
	return InstallConfig{
		PublicIP:       publicIP,
		LocalPath:      "./kubeconfig",
		Context:        "default",
		K3sChannel:     "stable",
		DisableTraefik: false,
	}
}

func Install(client *goss.Client, cfg InstallConfig) error {
	fmt.Println("  → Installing k3s...")

	installCmd := buildInstallCmd(cfg)

	out, _, err := ssh.Run(client, installCmd)
	if err != nil {
		return fmt.Errorf("k3s install failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)

	if err := waitForK3s(client); err != nil {
		return err
	}

	kubeconfig, token, err := fetchKubeconfig(client, cfg.PublicIP)
	if err != nil {
		return err
	}

	saveKubeconfig(kubeconfig, token, cfg)

	postInstallCIS(client, cfg)

	fmt.Println("  → k3s installed successfully!")
	return nil
}

func buildExtraArgs(cfg InstallConfig) string {
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
	return extraArgs
}

func buildEnvVars(cfg InstallConfig) string {
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
	return envVars
}

func buildInstallCmd(cfg InstallConfig) string {
	extraArgs := buildExtraArgs(cfg)
	envVars := buildEnvVars(cfg)

	serverFlags := fmt.Sprintf("--tls-san %s", cfg.PublicIP)
	if extraArgs != "" {
		serverFlags = serverFlags + " " + extraArgs
	}

	return fmt.Sprintf("curl -sfL https://get.k3s.io | %sINSTALL_K3S_EXEC='server %s' sudo sh -", envVars, serverFlags)
}

func waitForK3s(client *goss.Client) error {
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
	_, _, err := ssh.Run(client, waitCmd)
	if err != nil {
		return fmt.Errorf("k3s not ready: %w", err)
	}
	return nil
}

func fetchKubeconfig(client *goss.Client, publicIP string) (kubeconfig, token string, err error) {
	fmt.Println("  → Fetching kubeconfig...")
	kubeconfig, _, err = ssh.Run(client, "sudo cat /etc/rancher/k3s/k3s.yaml")
	if err != nil {
		return "", "", fmt.Errorf("fetch kubeconfig: %w", err)
	}

	kubeconfig = strings.ReplaceAll(kubeconfig, "127.0.0.1", publicIP)
	kubeconfig = strings.ReplaceAll(kubeconfig, "localhost", publicIP)

	token, _, err = ssh.Run(client, "sudo cat /var/lib/rancher/k3s/server/token")
	if err != nil {
		return "", "", fmt.Errorf("fetch token: %w", err)
	}

	return kubeconfig, strings.TrimSpace(token), nil
}

func saveKubeconfig(kubeconfig, token string, cfg InstallConfig) {
	if cfg.Merge {
		if err := ensureKubeconfigDir(); err != nil {
			log.Printf("create tmp dir: %v", err)
		}
		existing, err := readKubeconfig()
		if err == nil && len(existing) > 0 {
			kubeconfig = string(existing) + "\n" + kubeconfig
		}
		if err := writeKubeconfig([]byte(kubeconfig)); err != nil {
			log.Printf("write kubeconfig: %v", err)
		}
		fmt.Printf("  → Kubeconfig saved to /tmp/sdk-ops-kubeconfig (context: %s)\n", cfg.Context)
	} else {
		if err := os.WriteFile(filepath.Clean(cfg.LocalPath), []byte(kubeconfig), 0600); err != nil {
			log.Printf("write kubeconfig: %v", err)
		}
		fmt.Printf("  → Kubeconfig saved to %s\n", cfg.LocalPath)
	}

	fmt.Printf("  → Token: %s", token)
}

func postInstallCIS(client *goss.Client, cfg InstallConfig) {
	kcmd := `sudo kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml`

	if cfg.CISPSA {
		applyCISPSA(client, kcmd)
	}
	if cfg.CISAuditLog {
		applyCISAuditLog(client, kcmd)
	}
	if cfg.CISNetPol {
		applyCISNetPol(client, kcmd)
	}
	if cfg.CISSvcAcc {
		applyCISSvcAcc(client, kcmd)
	}
}

func applyCISPSA(client *goss.Client, kcmd string) {
	fmt.Println("  → CIS: enforcing Pod Security Admission (restricted)...")
	script := fmt.Sprintf(`
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

func applyCISAuditLog(client *goss.Client, _ string) {
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

func applyCISNetPol(client *goss.Client, kcmd string) {
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

func applyCISSvcAcc(client *goss.Client, kcmd string) {
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

func writeKubeconfig(data []byte) error {
	root, err := os.OpenRoot("/")
	if err != nil {
		return fmt.Errorf("open root: %w", err)
	}
	defer func() { if err := root.Close(); err != nil { log.Printf("k3s: root close error: %v", err) } }()
	f, err := root.OpenFile("tmp/sdk-ops-kubeconfig", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create kubeconfig: %w", err)
	}
	defer func() { if err := f.Close(); err != nil { log.Printf("k3s: file close error: %v", err) } }()
	_, err = f.Write(data)
	return err
}

func readKubeconfig() ([]byte, error) {
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}
	defer func() { if err := root.Close(); err != nil { log.Printf("k3s: root close error: %v", err) } }()
	f, err := root.Open("tmp/sdk-ops-kubeconfig")
	if err != nil {
		return nil, fmt.Errorf("open kubeconfig: %w", err)
	}
	defer func() { if err := f.Close(); err != nil { log.Printf("k3s: file close error: %v", err) } }()
	return io.ReadAll(f)
}

func ensureKubeconfigDir() error {
	root, err := os.OpenRoot("/")
	if err != nil {
		return err
	}
	defer func() { if err := root.Close(); err != nil { log.Printf("k3s: root close error: %v", err) } }()
	return root.Mkdir("tmp", 0750)
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
