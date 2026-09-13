package hardening

import (
	"fmt"
	"net"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// PeerProto guesses the transport for a fleet peer port: flannel's vxlan
// (8472) is UDP, everything else (etcd, apiserver, kubelet) is TCP.
func PeerProto(port int) string {
	if port == 8472 {
		return "udp"
	}
	return "tcp"
}

// ExposePeerPortDirect opens one peer port for one source IP with a plain,
// persisted nftables rule in the base hardening ruleset — the fleet path when
// the provider allowlist is not installed (e.g. k3s fleets with hardening and
// no CDN gating). Idempotent per (port, proto, ip): existing rules for the
// same tuple are replaced.
func ExposePeerPortDirect(client *goss.Client, port int, proto, ip string) error {
	// Defense in depth: the address is interpolated into a remote nftables
	// script — never accept anything that is not a plain IP.
	ip = strings.TrimSpace(ip)
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("peer expose: %q is not a valid IP", ip)
	}
	fam := "ip"
	if strings.Contains(ip, ":") {
		fam = "ip6"
	}
	addr := ip
	if !strings.Contains(addr, "/") {
		if fam == "ip" {
			addr += "/32"
		} else {
			addr += "/128"
		}
	}
	saddr := fmt.Sprintf("%s saddr %s", fam, addr)
	script := fmt.Sprintf(`if ! sudo nft list table inet filter >/dev/null 2>&1; then
  echo "peer expose: nftables table missing" >&2
  exit 1
fi
HANDLES=$(sudo nft --handle list chain inet filter input 2>/dev/null \
  | grep -E "dport %[1]d([^0-9]|$)" | grep -F "%[3]s" \
  | grep -oE 'handle [0-9]+' | awk '{print $2}')
for h in $HANDLES; do
  sudo nft delete rule inet filter input handle $h 2>/dev/null || true
done
sudo nft add rule inet filter input %[4]s dport %[1]d %[3]s accept
sudo nft list table inet filter | sudo tee /etc/nftables.conf > /dev/null
echo "peer %[1]d/%[4]s open for %[2]s (direct)"
`, port, ip, saddr, proto)
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("peer expose direct %d: %w\n%s", port, err, out)
	}
	fmt.Print("  " + strings.TrimSpace(out) + "\n")
	return nil
}
