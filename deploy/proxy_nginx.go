package deploy

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type NginxProxy struct{}

func (p *NginxProxy) Type() ProxyType {
	return ProxyNginx
}

func (p *NginxProxy) Install(client *goss.Client, cfg ProxyConfig) error {
	port := cfg.TargetPort
	if port == 0 {
		port = 8080
	}

	script := fmt.Sprintf(`
apt-get install -y -qq nginx 2>/dev/null || true

cat > /etc/nginx/sites-available/app << 'NGINXCONF'
server {
    listen 80;
    server_name %s;

    location / {
        proxy_pass http://localhost:%d;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINXCONF

ln -sf /etc/nginx/sites-available/app /etc/nginx/sites-enabled/ 2>/dev/null || true
rm -f /etc/nginx/sites-enabled/default
nginx -t 2>/dev/null && systemctl restart nginx || true

echo "Nginx configured for %s (-> :%d)"
`, cfg.Domain, port, cfg.Domain, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("nginx install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func (p *NginxProxy) UpdateTargetPort(client *goss.Client, domain string, port int) error {
	script := fmt.Sprintf(`cat > /etc/nginx/sites-available/app << 'NGINXCONF'
server {
    listen 80;
    server_name %s;
    location / {
        proxy_pass http://localhost:%d;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINXCONF
nginx -t 2>/dev/null && systemctl reload nginx || true
echo "Nginx updated to port %d"
`, domain, port, port)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("nginx update: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func (p *NginxProxy) Status(client *goss.Client) (string, error) {
	cmds := []string{
		"nginx -v 2>&1 || echo 'not installed'",
		"systemctl is-active nginx 2>/dev/null || echo 'stopped'",
	}
	out, _, err := ssh.Run(client, strings.Join(cmds, "; "))
	return out, err
}

func (p *NginxProxy) Remove(client *goss.Client) error {
	cmds := []string{
		"rm -f /etc/nginx/sites-available/app",
		"rm -f /etc/nginx/sites-enabled/app",
		"systemctl reload nginx 2>/dev/null || true",
		"echo 'Nginx app config removed'",
	}
	out, _, err := ssh.Run(client, strings.Join(cmds, "; "))
	fmt.Print(out)
	return err
}
