package hardening

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// RemoveCertRenew removes the cert renewal timer, the lego worker binary and
// the cert store from the node.
func RemoveCertRenew(client *goss.Client) error {
	script := `
sudo systemctl stop sdk-ops-certs.timer sdk-ops-certs.service 2>/dev/null || true
sudo systemctl disable sdk-ops-certs.timer 2>/dev/null || true
sudo rm -f /etc/systemd/system/sdk-ops-certs.service /etc/systemd/system/sdk-ops-certs.timer
sudo rm -rf /opt/sdk-ops/certs /etc/sdk-ops/certs
sudo systemctl daemon-reload
echo "cert renewal removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("cert renewal remove: %w", err)
	}
	fmt.Print(out)
	return nil
}

// CertRenewStatus reports the timer state and, when a domain is given, the
// expiry of the issued certificate from the central store.
func CertRenewStatus(client *goss.Client, domain string) (string, error) {
	store := "/etc/sdk-ops/certs"
	if domain != "" {
		store += "/" + domain
	}
	script := fmt.Sprintf(`
ACT=$(systemctl is-active sdk-ops-certs.timer 2>/dev/null || echo inactive)
LAST=$(systemctl show -p LastTriggerUSec --value sdk-ops-certs.timer 2>/dev/null || echo never)
echo "timer=$ACT last=$LAST"
if [ -f %s/cert.pem ]; then
  openssl x509 -in %s/cert.pem -noout -dates 2>/dev/null || echo "no cert yet"
else
  echo "no cert issued yet"
fi
tail -3 /var/log/sdk-ops-certs.log 2>/dev/null || echo "no log"
`, store, store)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("cert renewal status: %w", err)
	}
	return strings.TrimSpace(out), nil
}
