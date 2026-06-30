package deploy

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type CaddyProxy struct{}

func (p *CaddyProxy) Type() ProxyType {
	return ProxyCaddy
}

func (p *CaddyProxy) Install(client *goss.Client, cfg ProxyConfig) error {
	port := cfg.TargetPort
	if port == 0 {
		port = 8080
	}

	tlsBlock := fmt.Sprintf("tls %s", cfg.Email)
	if cfg.Staging {
		tlsBlock = fmt.Sprintf(`tls %s {
    issuer acme {
        dir https://acme-staging-v02.api.letsencrypt.org/directory
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

func (p *CaddyProxy) UpdateTargetPort(client *goss.Client, domain string, port int) error {
	script := fmt.Sprintf(`
Caddyfile="/etc/caddy/Caddyfile"
if [ -f "$Caddyfile" ]; then
    sed -i "s/reverse_proxy localhost:[0-9]*/reverse_proxy localhost:%d/" "$Caddyfile"
    systemctl reload caddy 2>/dev/null || caddy reload --config "$Caddyfile" 2>/dev/null || true
    echo "Caddy updated to port %d"
fi
`, port, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("caddy update: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func (p *CaddyProxy) Status(client *goss.Client) (string, error) {
	statusCmds := []string{
		"caddy version 2>/dev/null || echo 'not installed'",
		"systemctl is-active caddy 2>/dev/null || caddy --config /etc/caddy/CaddyFILE 2>/dev/null; echo 'running' || echo 'stopped'",
	}
	out, _, err := ssh.Run(client, strings.Join(statusCmds, "; "))
	return out, err
}

func (p *CaddyProxy) Remove(client *goss.Client) error {
	cmds := []string{
		"systemctl stop caddy 2>/dev/null || true",
		"caddy stop 2>/dev/null || true",
		"apt-get remove -y caddy 2>/dev/null || true",
		"rm -f /usr/local/bin/caddy",
		"rm -rf /etc/caddy",
		"rm -rf /var/lib/caddy",
		"echo 'Caddy removed'",
	}
	out, _, err := ssh.Run(client, strings.Join(cmds, "; "))
	fmt.Print(out)
	return err
}
