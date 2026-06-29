package hardening

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func FirewallOpen(client *goss.Client, port int, proto string) error {
	script := fmt.Sprintf(`
CURRENT=$(sudo nft list table inet filter 2>/dev/null || echo "")
if echo "$CURRENT" | grep -q "tcp dport {.*%[1]d"; then
    echo "port %[1]d already open"
    exit 0
fi
sudo nft add rule inet filter input tcp dport %[1]d accept
sudo nft list table inet filter | sudo tee /etc/nftables.conf >/dev/null 2>&1 || true
echo "port %[1]d/%[2]s opened"
`, port, proto)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("firewall open %d: %w\n%s", port, err, out)
	}
	fmt.Print(out)
	return nil
}

func FirewallClose(client *goss.Client, port int, proto string) error {
	script := fmt.Sprintf(`
RULES=$(sudo nft --handle list table inet filter 2>/dev/null | grep "tcp dport %[1]d" | grep -o 'handle [0-9]*' | awk '{print $2}')
if [ -z "$RULES" ]; then
    echo "port %[1]d not found in firewall"
    exit 0
fi
for h in $RULES; do
    sudo nft delete rule inet filter input handle $h 2>/dev/null || true
done
sudo nft list table inet filter | sudo tee /etc/nftables.conf >/dev/null 2>&1 || true
echo "port %[1]d/%[2]s closed"
`, port, proto)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("firewall close %d: %w\n%s", port, err, out)
	}
	fmt.Print(out)
	return nil
}

func FirewallList(client *goss.Client) (string, error) {
	script := `sudo nft list table inet filter 2>/dev/null || echo "no nftables rules found"`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("firewall list: %w", err)
	}
	return out, nil
}
