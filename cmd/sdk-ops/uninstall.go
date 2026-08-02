package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
)

// newInfraUninstallCmd: selectively uninstall sdk-ops components
// (docker, traefik, allowlist, swap, fail2ban, node-exporter, k3s, security, all).
func newInfraUninstallCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <component>",
		Short: "Selectively uninstall a component (docker|traefik|allowlist|swap|fail2ban|node-exporter|k3s|security|all)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			node := firewalledNode(cobraCmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			conn, err := infraConnect(node, f)
			if err != nil {
				return err
			}
			defer closeConn(conn)
			return uninstallComponent(conn, args[0])
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func uninstallComponent(conn *golang_ssh.Client, component string) error {
	switch strings.ToLower(component) {
	case "docker":
		script := `
sudo systemctl stop docker docker.socket 2>/dev/null || true
sudo systemctl disable docker docker.socket 2>/dev/null || true
DEBIAN_FRONTEND=noninteractive sudo apt-get remove -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin docker-compose-v2 2>/dev/null || true
echo "docker uninstalled (containers/images remain in /var/lib/docker)"
`
		return runUninstallScript(conn, script)
	case "traefik":
		script := `
sudo docker rm -f traefik 2>/dev/null || true
sudo rm -rf /etc/traefik /opt/traefik /srv/traefik-404
sudo systemctl stop traefik-404 2>/dev/null || true
sudo systemctl disable traefik-404 2>/dev/null || true
sudo rm -f /etc/systemd/system/traefik-404.service
sudo systemctl daemon-reload
echo "traefik uninstalled"
`
		return runUninstallScript(conn, script)
	case "allowlist":
		return hardening.RemoveAllowlist(conn)
	case "swap":
		return hardening.RemoveSwap(conn)
	case "fail2ban":
		script := `
sudo systemctl stop fail2ban 2>/dev/null || true
sudo systemctl disable fail2ban 2>/dev/null || true
DEBIAN_FRONTEND=noninteractive sudo apt-get remove -y -qq fail2ban 2>/dev/null || true
echo "fail2ban uninstalled"
`
		return runUninstallScript(conn, script)
	case "node-exporter", "node_exporter", "monitor":
		script := `
sudo systemctl stop node_exporter 2>/dev/null || true
sudo systemctl disable node_exporter 2>/dev/null || true
sudo rm -f /etc/systemd/system/node_exporter.service /usr/local/bin/node_exporter
sudo systemctl daemon-reload
echo "node_exporter uninstalled"
`
		return runUninstallScript(conn, script)
	case "k3s":
		script := `sudo /usr/local/bin/k3s-uninstall.sh 2>/dev/null || sudo /usr/local/bin/k3s-uninstaller.sh 2>/dev/null || echo "k3s uninstaller not found"; echo "k3s removed"`
		return runUninstallScript(conn, script)
	case "security":
		return hardening.RemoveSecurityWatch(conn)
	case "all":
		for _, c := range []string{"security", "allowlist", "traefik", "swap", "node-exporter", "fail2ban", "docker", "k3s"} {
			if err := uninstallComponent(conn, c); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown component %q (docker|traefik|allowlist|swap|fail2ban|node-exporter|k3s|security|all)", component)
	}
}

func runUninstallScript(conn *golang_ssh.Client, script string) error {
	out, _, err := ssh.Run(conn, script)
	if err != nil {
		return fmt.Errorf("uninstall: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}
