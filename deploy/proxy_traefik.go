package deploy

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type TraefikProxy struct{}

func (p *TraefikProxy) Type() ProxyType {
	return ProxyTraefik
}

func (p *TraefikProxy) Install(client *goss.Client, cfg ProxyConfig) error {
	port := cfg.TargetPort
	if port == 0 {
		port = 8080
	}

	appYml := fmt.Sprintf(`http:
  routers:
    app:
      rule: "Host(\x60%s\x60)"
      service: app
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
  services:
    app:
      loadBalancer:
        servers:
          - url: "http://localhost:%d"
`, cfg.Domain, port)

	script := fmt.Sprintf(`
mkdir -p /opt/traefik /etc/traefik/conf.d

cat > /etc/traefik/traefik.yml << 'EOF'
global:
  sendAnonymousUsage: false
api:
  dashboard: false
entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
providers:
  file:
    directory: /etc/traefik/conf.d
    watch: true
EOF

cat > /etc/traefik/conf.d/app.yml << 'EOF'
%s
EOF

docker rm -f traefik 2>/dev/null || true
docker run -d --name traefik \
  --restart unless-stopped \
  -p 80:80 -p 443:443 \
  -v /etc/traefik:/etc/traefik:ro \
  -v /opt/traefik:/opt/traefik \
  traefik:v3.0 --configFile=/etc/traefik/traefik.yml 2>/dev/null || docker run -d --name traefik \
  --restart unless-stopped \
  -p 80:80 -p 443:443 \
  -v /etc/traefik:/etc/traefik:ro \
  traefik:v3.0 --configFile=/etc/traefik/traefik.yml

echo "Traefik configured for %s (-> :%d)"
`, appYml, cfg.Domain, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("traefik install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func (p *TraefikProxy) UpdateTargetPort(client *goss.Client, domain string, port int) error {
	appYml := fmt.Sprintf(`http:
  routers:
    app:
      rule: "Host(\x60%s\x60)"
      service: app
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
  services:
    app:
      loadBalancer:
        servers:
          - url: "http://localhost:%d"
`, domain, port)

	script := fmt.Sprintf(`
cat > /etc/traefik/conf.d/app.yml << 'EOF'
%s
EOF
docker restart traefik 2>/dev/null || true
echo "Traefik updated to port %d"
`, appYml, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("traefik update: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func (p *TraefikProxy) Status(client *goss.Client) (string, error) {
	cmds := []string{
		"docker ps --filter name=traefik --format '{{.Image}} {{.Status}}' 2>/dev/null || echo 'not running'",
		"cat /etc/traefik/traefik.yml 2>/dev/null | head -3 || echo 'no config'",
	}
	out, _, err := ssh.Run(client, strings.Join(cmds, "; "))
	return out, err
}

func (p *TraefikProxy) Remove(client *goss.Client) error {
	cmds := []string{
		"docker rm -f traefik 2>/dev/null || true",
		"rm -rf /etc/traefik",
		"rm -rf /opt/traefik",
		"echo 'Traefik removed'",
	}
	out, _, err := ssh.Run(client, strings.Join(cmds, "; "))
	fmt.Print(out)
	return err
}
