package deploy

import (
	"fmt"
	"log"
	"os"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type CertProvider string

const (
	CertLetsEncrypt CertProvider = "letsencrypt"
	CertCloudflare  CertProvider = "cloudflare"
	CertManual      CertProvider = "manual"
)

type CertConfig struct {
	Domain     string
	Email      string
	Provider   CertProvider
	CertFile   string
	KeyFile    string
	TargetPort int
	Staging    bool
	Runtime    string
}

func InstallCert(client *goss.Client, cfg CertConfig) error {
	switch cfg.Provider {
	case CertLetsEncrypt:
		return installCertLetsEncrypt(client, cfg)
	case CertManual:
		return installCertManual(client, cfg)
	case CertCloudflare:
		return installCertCloudflare(client, cfg)
	default:
		return fmt.Errorf("unknown cert provider: %s", cfg.Provider)
	}
}

func installCertLetsEncrypt(client *goss.Client, cfg CertConfig) error {
	if cfg.Runtime == "k3s" || cfg.Runtime == "" {
		return installCertTraefik(client, cfg)
	}
	return installCertCaddy(client, cfg)
}

func installCertCaddy(client *goss.Client, cfg CertConfig) error {
	port := cfg.TargetPort
	if port == 0 {
		port = 8080
	}

	tlsBlock := fmt.Sprintf("tls %s", cfg.Email)
	if cfg.Staging {
		tlsBlock = fmt.Sprintf(`tls %s {
    issuer acme {
        ca https://acme-staging-v02.api.letsencrypt.org/directory
    }
}`, cfg.Email)
	}

	script := fmt.Sprintf(`
CADDY_VER="2.8.4"
if ! command -v caddy &>/dev/null; then
    apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https 2>/dev/null
    curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg 2>/dev/null
    echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" > /etc/apt/sources.list.d/caddy-stable.list
    apt-get update -qq && apt-get install -y -qq caddy 2>/dev/null || (
        cd /tmp
        curl -fsSLO "https://github.com/caddyserver/caddy/releases/download/v${CADDY_VER}/caddy_${CADDY_VER}_linux_amd64.tar.gz"
        tar xzf "caddy_${CADDY_VER}_linux_amd64.tar.gz"
        mv caddy /usr/local/bin/
    )
fi

mkdir -p /etc/caddy
cat > /etc/caddy/Caddyfile << 'CADDYFILE'
%s {
    %s
    reverse_proxy localhost:%d
}
CADDYFILE

systemctl enable caddy 2>/dev/null || true
systemctl restart caddy 2>/dev/null || caddy start --config /etc/caddy/Caddyfile
echo "Caddy configured for %s (-> :%d)"
`, cfg.Domain, tlsBlock, port, cfg.Domain, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("caddy install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func installCertTraefik(client *goss.Client, cfg CertConfig) error {
	if cfg.Email == "" {
		cfg.Email = "admin@" + cfg.Domain
	}

	script := fmt.Sprintf(`
mkdir -p /var/lib/rancher/k3s/server/manifests

cat > /var/lib/rancher/k3s/server/manifests/traefik-cert-%s.yaml << 'EOF'
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: %s-tls
  namespace: default
spec:
  secretName: %s-tls
  issuerRef:
    name: letsencrypt-%s
    kind: ClusterIssuer
  commonName: %s
  dnsNames:
  - %s
EOF

kubectl apply -f /var/lib/rancher/k3s/server/manifests/traefik-cert-%s.yaml 2>/dev/null
echo "Traefik certificate configured for %s"
`, cfg.Domain, cfg.Domain, cfg.Domain, cfg.Domain, cfg.Domain, cfg.Domain, cfg.Domain, cfg.Domain)

	checkIssuer, _, _ := ssh.Run(client, "kubectl get clusterissuer letsencrypt-prod 2>/dev/null || echo 'missing'")
	if strings.Contains(checkIssuer, "missing") {
		fmt.Println("  → Creating Let's Encrypt ClusterIssuer for Traefik...")
		issuerScript := fmt.Sprintf(`
cat << 'EOF' | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-%s
spec:
  acme:
    email: %s
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-%s-account-key
    solvers:
    - http01:
        ingress:
          class: traefik
EOF
`, cfg.Domain, cfg.Email, cfg.Domain)
		if _, _, err := ssh.Run(client, issuerScript); err != nil {
			return fmt.Errorf("create cluster issuer: %w", err)
		}
	}

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("traefik cert: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func installCertManual(client *goss.Client, cfg CertConfig) error {
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return fmt.Errorf("--cert-file and --key-file are required for manual cert")
	}

	certData, err := os.ReadFile(cfg.CertFile)
	if err != nil {
		return fmt.Errorf("read cert file: %w", err)
	}
	keyData, err := os.ReadFile(cfg.KeyFile)
	if err != nil {
		return fmt.Errorf("read key file: %w", err)
	}

	if err := uploadCertFile(client, certData, fmt.Sprintf("/etc/ssl/certs/%s.crt", cfg.Domain)); err != nil {
		return fmt.Errorf("upload cert: %w", err)
	}
	if err := uploadCertFile(client, keyData, fmt.Sprintf("/etc/ssl/certs/%s.key", cfg.Domain)); err != nil {
		return fmt.Errorf("upload key: %w", err)
	}

	fmt.Printf("  → Manual cert installed for %s\n", cfg.Domain)
	return nil
}

func uploadCertFile(client *goss.Client, data []byte, remotePath string) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer func() { if err := sess.Close(); err != nil { log.Printf("tls: session close error: %v", err) } }()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	go func() {
		defer func() { if err := stdin.Close(); err != nil { log.Printf("tls: stdin close error: %v", err) } }()
		if _, err := stdin.Write(data); err != nil { log.Printf("tls: stdin write error: %v", err) }
	}()

	out, err := sess.CombinedOutput(fmt.Sprintf("sudo mkdir -p /etc/ssl/certs && sudo sh -c 'cat > %s'", remotePath))
	if err != nil {
		return fmt.Errorf("upload: %w\n%s", err, string(out))
	}
	return nil
}

func installCertCloudflare(client *goss.Client, cfg CertConfig) error {
	fmt.Println("  → Cloudflare Origin CA: domain uses Cloudflare proxy")
	fmt.Println("  → Install Cloudflare Origin Certificate manually via CF dashboard")
	fmt.Println("  → Or use --provider letsencrypt for automatic cert")

	if cfg.Runtime == "k3s" || cfg.Runtime == "" {
		script := `
kubectl annotate ingress --all kubernetes.io/ingress.class=traefik 2>/dev/null || true
echo "  → Marked ingresses for Traefik with Cloudflare proxy"
`
		out, _, err := ssh.Run(client, script)
		if err != nil {
			return fmt.Errorf("cloudflare config: %w", err)
		}
		fmt.Print(out)
	}

	return nil
}

func GetCertInfo(client *goss.Client, domain string) (string, error) {
	script := fmt.Sprintf(`
echo "=== Cert info for %s ==="
CADDY_DATA="/var/lib/caddy/.local/share/caddy/certificates"
if [ -d "$CADDY_DATA" ]; then
    find "$CADDY_DATA" -name "*.pem" 2>/dev/null | head -5
    for cert in $(find "$CADDY_DATA" -name "%s" -type d 2>/dev/null); do
        ls -la "$cert/"
        openssl x509 -in "$cert/%s.crt" -noout -text 2>/dev/null | grep -E "Not Before|Not After|Subject:" || true
    done
else
    echo "No Caddy certificates directory found"
    caddy cert-info 2>/dev/null || echo "Check /etc/caddy/Caddyfile"
fi
`, domain, domain, domain)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("cert info: %w", err)
	}
	return out, nil
}

func UninstallCaddy(client *goss.Client) error {
	script := `
systemctl stop caddy 2>/dev/null || true
caddy stop 2>/dev/null || true
apt-get remove -y caddy 2>/dev/null || true
rm -f /usr/local/bin/caddy
rm -rf /etc/caddy
rm -rf /var/lib/caddy
echo "Caddy removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("caddy remove: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}
