package deploy

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type CaddyConfig struct {
	Domain      string
	Email       string
	TargetPort  int
	Staging     bool
}

func InstallCaddy(client *goss.Client, cfg CaddyConfig) error {
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
if command -v caddy &>/dev/null; then
    echo "Caddy already installed"
else
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
cat > /etc/caddy/Caddyfile << CADDYFILE
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
