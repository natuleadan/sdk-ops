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

// baseTraefikScript renders the shared Traefik base: config, catch-all 404
// responder, and the container. An empty domain skips the app router.
func baseTraefikScript(cfg ProxyConfig) string {
	appYml := ""
	summary := ""
	if cfg.Domain != "" {
		appYml = fmt.Sprintf(`
sudo tee /etc/traefik/conf.d/app.yml > /dev/null << 'APPEOF'
http:
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
APPEOF
`, cfg.Domain, cfg.TargetPort)
		summary = " for " + cfg.Domain
	}

	return fmt.Sprintf(`
set -e
sudo mkdir -p /etc/traefik/conf.d /opt/traefik /srv/traefik-404

sudo tee /etc/traefik/traefik.yml > /dev/null << 'EOF'
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

# Catch-all: any undeclared host goes to the 404 responder (port 80).
sudo tee /etc/traefik/conf.d/00-catchall.yml > /dev/null << 'EOF'
http:
  routers:
    catchall:
      rule: "HostRegexp(\x60{host:.+}\x60)"
      priority: 1
      entryPoints:
        - web
      service: notfound
  services:
    notfound:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:18080"
EOF

sudo tee /srv/traefik-404/server.py > /dev/null << 'PYEOF'
from http.server import BaseHTTPRequestHandler, HTTPServer

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"<html><body style=\"font-family:sans-serif;text-align:center;margin-top:20%%\"><h1>404</h1><p>Recurso no disponible</p></body></html>"
        self.send_response(404)
        self.send_header("Content-Type", "text/html")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass

HTTPServer(("127.0.0.1", 18080), H).serve_forever()
PYEOF

sudo tee /etc/systemd/system/traefik-404.service > /dev/null << 'UNITEOF'
[Unit]
Description=Traefik catch-all 404 responder

[Service]
Type=simple
ExecStart=/usr/bin/python3 -u /srv/traefik-404/server.py
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
UNITEOF
sudo systemctl daemon-reload
sudo systemctl enable --now traefik-404

sudo docker rm -f traefik 2>/dev/null || true
sudo docker run -d --name traefik \
  --restart unless-stopped \
  -p 80:80 -p 443:443 \
  -v /etc/traefik:/etc/traefik:ro \
  -v /opt/traefik:/opt/traefik \
  traefik:v3.0 --configFile=/etc/traefik/traefik.yml 2>/dev/null || sudo docker run -d --name traefik \
  --restart unless-stopped \
  -p 80:80 -p 443:443 \
  -v /etc/traefik:/etc/traefik:ro \
  traefik:v3.0 --configFile=/etc/traefik/traefik.yml
%s
sudo docker restart traefik 2>/dev/null || true
echo "Traefik ready (catch-all 404 active)%s"
`, appYml, summary)
}

func (p *TraefikProxy) Install(client *goss.Client, cfg ProxyConfig) error {
	port := cfg.TargetPort
	if port == 0 {
		port = 8080
	}
	cfg.TargetPort = port

	script := baseTraefikScript(cfg)
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
