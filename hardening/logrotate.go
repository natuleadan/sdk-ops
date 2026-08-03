package hardening

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// logRotationConfig is the logrotate config for the sdk-ops logs: rotate
// monthly OR when a log reaches 100MB, keep the last 4 rotated files
// compressed (evidence retained, disk bounded).
const logRotationConfig = `/var/log/sdk-ops-*.log {
    monthly
    maxsize 100M
    rotate 4
    compress
    missingok
    notifempty
    copytruncate
    su root root
}
`

// LogRotationConfig returns the logrotate config content (used by the ops
// CLI to compare the installed file against the template).
func LogRotationConfig() string { return logRotationConfig }

// InstallLogRotation installs the logrotate config for the sdk-ops logs.
func InstallLogRotation(client *goss.Client) error {
	script := `
sudo tee /etc/logrotate.d/sdk-ops > /dev/null << 'ROTATEEOF'
` + logRotationConfig + `ROTATEEOF
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

// RemoveLogRotation removes the logrotate config for the sdk-ops logs.
func RemoveLogRotation(client *goss.Client) error {
	script := `
sudo rm -f /etc/logrotate.d/sdk-ops
echo "logrotate: sdk-ops config removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("logrotate remove: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}
