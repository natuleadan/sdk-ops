package k3s

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

// HelmVersion is the helm release installed by the provision on every k3s
// host. Templates verify this version instead of downloading helm themselves,
// so the whole fleet shares one pinned binary.
const HelmVersion = "v3.15.4"

// EnsureHelmOn installs the pinned helm binary when the node does not have it
// (or has a different version). The provision owns the install; the service
// templates (nats-cluster, etcd-cluster, crowdsec-cluster) only verify it.
// Idempotent: a matching version is a no-op.
func EnsureHelmOn(client *goss.Client, version string) error {
	if version == "" {
		version = HelmVersion
	}
	out, _, err := ssh.Run(client, helmEnsureScript(version))
	if err != nil {
		return fmt.Errorf("helm ensure: %w", err)
	}
	fmt.Print("  " + strings.TrimSpace(out) + "\n")
	return nil
}

// helmEnsureScript renders the idempotent helm install. It detects the arch,
// skips when the pinned version is already present and otherwise downloads the
// official get.helm.sh tarball (curl or wget). sudo is used only when not root
// (hardened fleets run as the sdkops user).
func helmEnsureScript(version string) string {
	return fmt.Sprintf(`WANT=%q
if command -v helm >/dev/null 2>&1 && helm version --short 2>/dev/null | grep -q "$WANT"; then
  echo "helm: $WANT already installed"
  exit 0
fi
case "$(uname -m)" in
  x86_64|amd64)   HARCH=amd64 ;;
  aarch64|arm64)  HARCH=arm64 ;;
  *) echo "helm: unsupported arch $(uname -m)"; exit 1 ;;
esac
URL="https://get.helm.sh/helm-${WANT}-linux-${HARCH}.tar.gz"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$URL" -o /tmp/helm.tgz || { echo "helm: download failed"; exit 1; }
elif command -v wget >/dev/null 2>&1; then
  wget -qO /tmp/helm.tgz "$URL" || { echo "helm: download failed"; exit 1; }
else
  echo "helm: curl or wget required"; exit 1
fi
SUDO=""
[ "$(id -u)" != "0" ] && SUDO="sudo"
tar -xzf /tmp/helm.tgz -C /tmp
${SUDO} install -m 0755 "/tmp/linux-${HARCH}/helm" /usr/local/bin/helm
rm -rf /tmp/helm.tgz "/tmp/linux-${HARCH}"
echo "helm: installed ${WANT}"`, version)
}
