package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type AlertmanagerConfig struct {
	SlackWebhookURL string
	Email           string
	TelegramToken    string
	TelegramChatID   string
}

func (c AlertmanagerConfig) HasReceiver() bool {
	return c.SlackWebhookURL != "" || c.Email != "" || (c.TelegramToken != "" && c.TelegramChatID != "")
}

func InstallAlertmanager(client *goss.Client, cfg AlertmanagerConfig) error {
	script := `
AM_VER="0.27.0"
if command -v alertmanager &>/dev/null; then
    echo "Alertmanager already installed"
    exit 0
fi
cd /tmp
curl -fsSLO "https://github.com/prometheus/alertmanager/releases/download/v${AM_VER}/alertmanager-${AM_VER}.linux-amd64.tar.gz"
tar xzf "alertmanager-${AM_VER}.linux-amd64.tar.gz"
cp "alertmanager-${AM_VER}.linux-amd64/alertmanager" /usr/local/bin/
cp "alertmanager-${AM_VER}.linux-amd64/amtool" /usr/local/bin/
rm -rf "alertmanager-${AM_VER}.linux-amd64" "alertmanager-${AM_VER}.linux-amd64.tar.gz"
mkdir -p /etc/alertmanager /var/lib/alertmanager
echo "Alertmanager binary installed"
`

	configContent := `route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 5m
  repeat_interval: 1h
  receiver: 'default'

receivers:
  - name: 'default'
`

	if cfg.SlackWebhookURL != "" {
		configContent += fmt.Sprintf(`
    slack_configs:
      - api_url: %q
        channel: '#alerts'
        send_resolved: true
        title: '{{ .GroupLabels.alertname }}'
        text: '{{ .CommonAnnotations.description }}'
`, cfg.SlackWebhookURL)
	}

	if cfg.Email != "" {
		configContent += fmt.Sprintf(`
    email_configs:
      - to: %q
        send_resolved: true
`, cfg.Email)
	}

	if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
		configContent += fmt.Sprintf(`
    telegram_configs:
      - bot_token: %q
        chat_id: %d
        send_resolved: true
        parse_mode: 'HTML'
`, cfg.TelegramToken, parseInt64(cfg.TelegramChatID))
	}

	script += fmt.Sprintf(`
cat > /etc/alertmanager/alertmanager.yml << 'CONFIG'
%s
CONFIG

cat > /etc/systemd/system/alertmanager.service << 'SERVICE'
[Unit]
Description=Prometheus Alertmanager
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/alertmanager --config.file=/etc/alertmanager/alertmanager.yml --storage.path=/var/lib/alertmanager
Restart=always

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable alertmanager
systemctl restart alertmanager
echo "Alertmanager configured and started"
`, configContent)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("alertmanager install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func InstallAlertRule(client *goss.Client, ruleFilePath string) error {
	data, err := os.ReadFile(ruleFilePath)
	if err != nil {
		return fmt.Errorf("read rule file: %w", err)
	}

	script := fmt.Sprintf(`
mkdir -p /etc/alertmanager/rules
cat > /etc/alertmanager/rules/custom.yml << 'RULE'
%s
RULE

killall -HUP alertmanager 2>/dev/null || systemctl reload alertmanager 2>/dev/null || true
echo "Alert rule installed: %s"
`, string(data), filepath.Base(ruleFilePath))

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("install rule: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func UninstallAlertmanager(client *goss.Client) error {
	script := `
systemctl stop alertmanager 2>/dev/null || true
systemctl disable alertmanager 2>/dev/null || true
rm -f /usr/local/bin/alertmanager /usr/local/bin/amtool
rm -rf /etc/alertmanager /var/lib/alertmanager
rm -f /etc/systemd/system/alertmanager.service
systemctl daemon-reload
echo "Alertmanager removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("alertmanager remove: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}
