package deploy

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type PromtailConfig struct {
	LokiURL  string
	NodeName string
	Port     int
}

func InstallPromtail(client *goss.Client, cfg PromtailConfig) error {
	nodeName := cfg.NodeName
	if nodeName == "" {
		nodeName = "node"
	}
	lokiURL := cfg.LokiURL
	if lokiURL == "" {
		return fmt.Errorf("loki URL is required")
	}
	port := cfg.Port
	if port == 0 {
		port = 9080
	}

	script := fmt.Sprintf(`
PROMTAIL_VER="3.0.0"
if command -v promtail &>/dev/null; then
    echo "Promtail already installed"
    exit 0
fi
cd /tmp
curl -fsSLO "https://github.com/grafana/loki/releases/download/v${PROMTAIL_VER}/promtail-linux-amd64.zip"
apt-get install -y -qq unzip 2>/dev/null || true
unzip -o promtail-linux-amd64.zip 2>/dev/null
mv promtail-linux-amd64 /usr/local/bin/promtail 2>/dev/null || true
rm -f promtail-linux-amd64.zip

mkdir -p /etc/promtail
cat > /etc/promtail/promtail.yaml << 'CONFIG'
server:
  http_listen_port: %d
  grpc_listen_port: 0

positions:
  filename: /var/log/promtail-positions.yaml

clients:
  - url: %s/loki/api/v1/push

scrape_configs:
  - job_name: system
    static_configs:
      - targets: [localhost]
        labels:
          job: varlogs
          __path__: /var/log/*.log
          host: %s
  - job_name: journal
    journal:
      path: /var/log/journal
      labels:
        job: systemd-journal
        host: %s
CONFIG

cat > /etc/systemd/system/promtail.service << 'SERVICE'
[Unit]
Description=Promtail log shipper
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/promtail -config.file=/etc/promtail/promtail.yaml
Restart=always

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable promtail
systemctl restart promtail
echo "Promtail installed -> %s (port %d)"
`, port, lokiURL, nodeName, nodeName, lokiURL, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("promtail install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func UninstallPromtail(client *goss.Client) error {
	script := `
systemctl stop promtail 2>/dev/null || true
systemctl disable promtail 2>/dev/null || true
rm -f /usr/local/bin/promtail
rm -rf /etc/promtail
rm -f /etc/systemd/system/promtail.service
systemctl daemon-reload
echo "Promtail removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("promtail remove: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}
