package hardening

import (
	"fmt"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// applySwapScript computes and applies the swap file with the bottom-up rule:
// base 0.5x RAM (always), +0.5x RAM per 10GB of FREE disk, capped at 2x RAM.
// Existing swapfiles are resized to the computed size.
const applySwapScript = `
MEM_MB=$(free -m | awk '/^Mem:/{print $2}')
FREE_MB=$(df -m / | awk 'NR==2{print $4}')

# bottom-up: 0.5x base, +0.5x per 10GB free, cap 2x (4 half-units)
STEPS=$((FREE_MB / 10240))
MULT_HALF=$((1 + STEPS))
[ "$MULT_HALF" -gt 4 ] && MULT_HALF=4
SWAP_MB=$((MEM_MB * MULT_HALF / 2))

if [ "$SWAP_MB" -lt 256 ]; then
  echo "swap: skipped (computed ${SWAP_MB}M too small)"
  exit 0
fi

CUR_MB=0
[ -f /swapfile ] && CUR_MB=$(( $(stat -c%s /swapfile) / 1024 / 1024 ))

if [ "$CUR_MB" -ne "$SWAP_MB" ]; then
  sudo swapoff /swapfile 2>/dev/null || true
  sudo rm -f /swapfile
  sudo fallocate -l ${SWAP_MB}M /swapfile 2>/dev/null || sudo dd if=/dev/zero of=/swapfile bs=1M count=${SWAP_MB} 2>/dev/null
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile 2>/dev/null
fi
sudo swapon /swapfile 2>/dev/null || true
sudo grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab > /dev/null
echo "swap: OK (${SWAP_MB}M = ${MULT_HALF}/2x of ${MEM_MB}M RAM, ${FREE_MB}M free disk)"
`

// ApplySwap creates or resizes the swap file using the bottom-up rule.
func ApplySwap(client *goss.Client) error {
	return ssh.RunStream(client, applySwapScript)
}

// RemoveSwap disables and deletes the swap file.
func RemoveSwap(client *goss.Client) error {
	script := `
sudo swapoff /swapfile 2>/dev/null || true
sudo rm -f /swapfile
sudo sed -i '/^\/swapfile/d' /etc/fstab 2>/dev/null || true
echo "swap removed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("swap remove: %w", err)
	}
	fmt.Print(out)
	return nil
}

// SwapStatus reports the current swap state on the node.
func SwapStatus(client *goss.Client) (string, error) {
	script := `
free -m | awk '/^Swap/{print "swap active: " $2 "M total, " $3 "M used"}'
ls -la /swapfile 2>/dev/null | awk '{print "swapfile: " $5 " bytes"}'
grep '^/swapfile' /etc/fstab 2>/dev/null && echo "fstab: persistent" || echo "fstab: not configured"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return "", fmt.Errorf("swap status: %w", err)
	}
	return out, nil
}
