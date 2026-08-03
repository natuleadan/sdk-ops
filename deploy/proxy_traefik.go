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

// baseTraefikScript renders the shared Traefik base: config and the
// container. The container is created once and only recreated when a
// declared env var is missing (Docker's restart policy keeps it alive).
// No catch-all router is installed on :80 — it would swallow the ACME
// challenges. Apps are routed via the file provider (watch: true) or docker
// labels, so config changes never need a restart.
func baseTraefikScript(client *goss.Client, cfg ProxyConfig) string {
	// IPv6-only hosts: the docker bridge has no v4 DNS route, so Traefik must
	// run on the host network (uses the host's v6 DNS + listeners).
	netArgs := "-p 80:80 -p 443:443"
	catchallTarget := "http://172.17.0.1:18080"
	if !hasIPv4(client) {
		netArgs = "--network host"
		catchallTarget = "http://127.0.0.1:18080"
	}
	envArgs := ""
	for _, e := range cfg.Env {
		envArgs += " -e " + e
	}
	runCmd := fmt.Sprintf(`sudo docker run -d --name traefik --restart unless-stopped %s%s -v /etc/traefik:/etc/traefik:ro -v /opt/traefik:/opt/traefik traefik:v3.2 --configFile=/etc/traefik/traefik.yml`, netArgs, envArgs)

	envCheck := "ENVS_OK=1"
	for _, e := range cfg.Env {
		name := strings.SplitN(e, "=", 2)[0]
		envCheck += fmt.Sprintf(`
if ! docker inspect traefik --format '{{range .Config.Env}}{{.}}{{"\n"}}{{end}}' | grep -q '^%s='; then ENVS_OK=0; fi`, name)
	}
	containerScript := fmt.Sprintf(`
if docker inspect traefik >/dev/null 2>&1; then
  %s
  if [ "$ENVS_OK" != "1" ]; then
    echo "traefik: recreating container (missing env)"
    sudo docker rm -f traefik >/dev/null 2>&1 || true
    %s
  else
    echo "traefik: container already running"
  fi
else
  %s
fi
`, envCheck, runCmd, runCmd)

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

# 404 responder for unknown hosts. It is only wired on websecure (443):
# a router on :80 would swallow the ACME challenges and certificates could
# never renew. The responder binds 0.0.0.0 so bridge-mode Traefik reaches it
# via the docker gateway and host-network Traefik via localhost.
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

HTTPServer(("0.0.0.0", 18080), H).serve_forever()
PYEOF

sudo tee /etc/systemd/system/traefik-404.service > /dev/null << 'UNITEOF'
[Unit]
Description=Traefik 404 responder (websecure catch-all)

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

# Catch-all on websecure ONLY (the :80 router was removed — it broke ACME).
sudo tee /etc/traefik/conf.d/00-catchall.yml > /dev/null << 'EOF'
http:
  routers:
    catchall:
      rule: "HostRegexp(\x60.+\x60)"
      priority: 1
      entryPoints:
        - websecure
      tls: {}
      service: notfound
  services:
    notfound:
      loadBalancer:
        servers:
          - url: "%s"
EOF

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

%s

# Shared network so bridge-mode Traefik resolves service names (docker DNS).
sudo docker network create sdk-ops-net >/dev/null 2>&1 || true
NET=$(docker inspect traefik --format '{{.HostConfig.NetworkMode}}' 2>/dev/null || echo host)
if [ "$NET" != "host" ]; then
  sudo docker network connect sdk-ops-net traefik >/dev/null 2>&1 || true
fi
%s
echo "Traefik ready%s"
`, catchallTarget, containerScript, appYml, summary)
}

func (p *TraefikProxy) Install(client *goss.Client, cfg ProxyConfig) error {
	port := cfg.TargetPort
	if port == 0 {
		port = 8080
	}
	cfg.TargetPort = port

	script := baseTraefikScript(client, cfg)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("traefik install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// hasIPv4 reports whether the node has a public IPv4 address (docker0 and
// other private bridges do not count — IPv6-only hosts must use host network).
func hasIPv4(client *goss.Client) bool {
	out, _, err := ssh.Run(client, `ip -4 addr show 2>/dev/null | awk '/inet / && $2 !~ /^127\./ && $2 !~ /^10\./ && $2 !~ /^172\.(1[6-9]|2[0-9]|3[01])\./ && $2 !~ /^192\.168\./' | wc -l`)
	return err == nil && strings.TrimSpace(out) != "0" && strings.TrimSpace(out) != ""
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
