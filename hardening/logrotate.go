package hardening

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// InstallLogRotation installs a logrotate config for the sdk-ops logs:
// rotate monthly OR when a log reaches 100MB, keep the last 4 rotated
// files compressed (evidence retained, disk bounded).
func InstallLogRotation(client *goss.Client) error {
	script := `
sudo tee /etc/logrotate.d/sdk-ops > /dev/null << 'ROTATEEOF'
/var/log/sdk-ops-*.log {
    monthly
    maxsize 100M
    rotate 4
    compress
    missingok
    notifempty
    copytruncate
    su root root
}
ROTATEEOF
sudo chmod 0644 /etc/logrotate.d/sdk-ops
echo "logrotate: sdk-ops config installed (monthly / 100M, keep 4)"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("logrotate install: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}
