package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
)

// ProvisionFile is the batch provisioning YAML: N hosts initialized in
// parallel, with optional peer port relationships between them.
type ProvisionFile struct {
	Mode              string          `yaml:"mode"`
	Parallel          int             `yaml:"parallel"`
	FirewallAllowlist string          `yaml:"firewall_allowlist"`
	AdminIPs          string          `yaml:"admin_ips"`
	NoTraefik         bool            `yaml:"no_traefik"`
	Hosts             []ProvisionHost `yaml:"hosts"`
	Peers             []ProvisionPeer `yaml:"peers"`
	Bans              []string        `yaml:"bans"`
	Telegram          TelegramConfig  `yaml:"telegram"`
}

// TelegramConfig enables Telegram alerts for allowlist refresh failures.
type TelegramConfig struct {
	Enabled bool   `yaml:"enabled"`
	APIKey  string `yaml:"api_key"`
	ChatID  string `yaml:"chat_id"`
}

// ProvisionHost is a single VPS entry. Per-host values override the globals.
type ProvisionHost struct {
	Name              string `yaml:"name"`
	Host              string `yaml:"host"`
	PeerIP            string `yaml:"peer_ip,omitempty"`
	User              string `yaml:"user"`
	SSHKey            string `yaml:"ssh_key"`
	Port              int    `yaml:"port"`
	FirewallAllowlist string `yaml:"firewall_allowlist,omitempty"`
	AdminIPs          string `yaml:"admin_ips,omitempty"`
}

// ProvisionPeer opens ports on host To restricted to host From's IP.
type ProvisionPeer struct {
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Ports []int  `yaml:"ports"`
}

type provisionResult struct {
	Name  string
	Host  string
	Error error
}

func newProvisionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provision <file.yaml>",
		Short: "Provision multiple VPSes from a YAML fleet file",
		Long: `Provision multiple VPSes from a single YAML file.

Each host runs the full init (hardening + swap + Docker + Traefik + optional
provider allowlist) in parallel (--parallel). The peers section opens ports
between hosts, restricted to each other's IPs.

Example provision.yaml:
  mode: docker
  parallel: 3
  firewall_allowlist: cf
  admin_ips: "203.0.113.10,2001:db8::1"
  hosts:
    - name: netcup
      host: 152.53.169.115
      peer_ip: 2a0a:4cc0:2000:a9ea:2464:ccff:fedb:f581
      user: root
      ssh_key: ~/.ssh/id_ed25519
    - name: vps-lite
      host: 2a03:4000:28:4f9:8ab:80ff:fe09:e007
      user: root
      ssh_key: ~/.ssh/id_ed25519
  peers:
    - from: vps-lite
      to: netcup
      ports: [43453]
    - from: netcup
      to: vps-lite
      ports: [3454532]`,
		Args: cobra.ExactArgs(1),
		RunE: runProvision,
	}
	return cmd
}

func runProvision(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(filepath.Clean(args[0]))
	if err != nil {
		return fmt.Errorf("read provision file: %w", err)
	}
	var pf ProvisionFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("parse provision file: %w", err)
	}
	if err := normalizeProvision(&pf); err != nil {
		return err
	}
	parallel := pf.Parallel
	if parallel <= 0 {
		parallel = 1
	}

	names, err := validateProvision(&pf)
	if err != nil {
		return err
	}

	fmt.Printf("→ Provisioning %d hosts (mode=%s, parallel=%d)\n", len(pf.Hosts), pf.Mode, parallel)

	results := provisionHosts(pf, parallel)
	failed := countProvisionFailures(results)
	if failed > 0 {
		return fmt.Errorf("%d/%d hosts failed to provision", failed, len(results))
	}

	// Peers phase: open the requested ports between hosts, restricted to the
	// peer IP (requires the allowlist, which the init installed).
	if len(pf.Peers) > 0 {
		fmt.Println("\n→ Configuring peers...")
		for _, peer := range pf.Peers {
			if err := applyProvisionPeer(pf, names, peer); err != nil {
				return err
			}
		}
		fmt.Println("→ Peers configured")
	}

	// Bans phase: ban explicit IPs on every host (fail2ban).
	if len(pf.Bans) > 0 {
		if err := applyProvisionBans(pf); err != nil {
			return err
		}
	}

	// Telegram phase: write notify.env on every host for refresh alerts.
	if pf.Telegram.Enabled {
		if err := applyTelegramNotify(pf); err != nil {
			return err
		}
	}

	fmt.Println("\n✅ Provision complete")
	return nil
}

// applyTelegramNotify writes the notify.env used by the allowlist updater to
// alert on refresh failures via Telegram.
func applyTelegramNotify(pf ProvisionFile) error {
	fmt.Println("\n→ Configuring Telegram alerts...")
	for _, h := range pf.Hosts {
		port := h.Port
		if port == 0 {
			port = 22
		}
		f := infraFlags{user: "sdkops", key: h.SSHKey, port: port, mode: pf.Mode}
		conn, err := infraConnect(h.Host, &f)
		if err != nil {
			return fmt.Errorf("telegram: connect %s: %w", h.Name, err)
		}
		script := fmt.Sprintf(`sudo tee /etc/sdk-ops/firewall/notify.env > /dev/null << 'EOF'
export TELEGRAM_API_KEY=%s
export TELEGRAM_CHAT_ID=%s
EOF
sudo chmod 0600 /etc/sdk-ops/firewall/notify.env
echo "telegram: %s configured"`, pf.Telegram.APIKey, pf.Telegram.ChatID, h.Name)
		if _, _, err := ssh.Run(conn, script); err != nil {
			closeConn(conn)
			return fmt.Errorf("telegram: %s: %w", h.Name, err)
		}
		fmt.Printf("  → telegram alerts on %s\n", h.Name)
		closeConn(conn)
	}
	fmt.Println("→ Telegram configured")
	return nil
}

// applyProvisionBans bans every explicit IP on every host.
func applyProvisionBans(pf ProvisionFile) error {
	fmt.Println("\n→ Applying bans...")
	for _, h := range pf.Hosts {
		port := h.Port
		if port == 0 {
			port = 22
		}
		f := infraFlags{user: "sdkops", key: h.SSHKey, port: port, mode: pf.Mode}
		conn, err := infraConnect(h.Host, &f)
		if err != nil {
			return fmt.Errorf("bans: connect %s: %w", h.Name, err)
		}
		defer closeConn(conn)
		for _, b := range pf.Bans {
			fmt.Printf("  → banning %s on %s\n", b, h.Name)
			if err := hardening.Fail2banBan(conn, b); err != nil {
				return err
			}
		}
	}
	fmt.Println("→ Bans applied")
	return nil
}

// applyProvisionPeer opens the peer ports on the target host for the source
// host IP (scope ips).
func applyProvisionPeer(pf ProvisionFile, names map[string]string, peer ProvisionPeer) error {
	toHost, err := findProvisionHost(pf.Hosts, peer.To)
	if err != nil {
		return err
	}
	fromIP := names[peer.From]
	port := toHost.Port
	if port == 0 {
		port = 22
	}
	f := infraFlags{
		user: "sdkops",
		key:  toHost.SSHKey,
		port: port,
		mode: pf.Mode,
	}
	conn, err := infraConnect(toHost.Host, &f)
	if err != nil {
		return fmt.Errorf("peer %s: connect %s: %w", peer.From, peer.To, err)
	}
	defer closeConn(conn)
	for _, port := range peer.Ports {
		// Idempotent: clear any previous rules for the port first (the fleet
		// file is the single source of truth for peers).
		_ = hardening.AllowlistUnexposePort(conn, port)
		fmt.Printf("  → %s can reach %s:%d\n", peer.From, peer.To, port)
		if err := hardening.AllowlistExposePort(conn, port, "tcp", hardening.PortScopeIPs, fromIP); err != nil {
			return fmt.Errorf("peer %s -> %s port %d: %w", peer.From, peer.To, port, err)
		}
	}
	return nil
}

// provisionHosts runs every host init with a parallelism limit.
func provisionHosts(pf ProvisionFile, parallel int) []provisionResult {
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	results := make([]provisionResult, len(pf.Hosts))
	for i, h := range pf.Hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, h ProvisionHost) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = provisionHost(pf, h)
		}(i, h)
	}
	wg.Wait()
	return results
}

// countProvisionFailures reports and counts the failed hosts.
func countProvisionFailures(results []provisionResult) int {
	failed := 0
	for _, r := range results {
		if r.Error != nil {
			failed++
			fmt.Printf("✗ %s (%s): %v\n", r.Name, r.Host, r.Error)
		} else {
			fmt.Printf("✓ %s (%s)\n", r.Name, r.Host)
		}
	}
	return failed
}

// normalizeProvision applies defaults and validates the fleet globals.
func normalizeProvision(pf *ProvisionFile) error {
	if len(pf.Hosts) == 0 {
		return fmt.Errorf("provision file has no hosts")
	}
	if pf.Mode == "" {
		pf.Mode = "docker"
	}
	if pf.Mode != "k3s" && pf.Mode != "docker" && pf.Mode != "bare" {
		return fmt.Errorf("invalid mode %q (k3s, docker, bare)", pf.Mode)
	}
	return nil
}

// validateProvision validates the fleet file and returns name->host IP map.
func validateProvision(pf *ProvisionFile) (map[string]string, error) {
	names := map[string]string{}
	for _, h := range pf.Hosts {
		if h.Name == "" || h.Host == "" {
			return nil, fmt.Errorf("every host needs name and host")
		}
		peerIP := h.Host
		if h.PeerIP != "" {
			if _, err := hardening.ValidateCIDR(strings.TrimSpace(h.PeerIP)); err != nil {
				return nil, fmt.Errorf("host %q peer_ip %q is not a valid IP: %v", h.Name, h.PeerIP, err)
			}
			peerIP = strings.TrimSpace(h.PeerIP)
		}
		names[h.Name] = peerIP
	}
	for _, peer := range pf.Peers {
		if names[peer.From] == "" {
			return nil, fmt.Errorf("peer from %q not found in hosts", peer.From)
		}
		if names[peer.To] == "" {
			return nil, fmt.Errorf("peer to %q not found in hosts", peer.To)
		}
		if len(peer.Ports) == 0 {
			return nil, fmt.Errorf("peer %s -> %s has no ports", peer.From, peer.To)
		}
	}
	for _, b := range pf.Bans {
		if _, err := hardening.ValidateCIDR(strings.TrimSpace(b)); err != nil {
			return nil, fmt.Errorf("bans entry %q is not a valid IP/CIDR: %v", b, err)
		}
	}
	if pf.Telegram.Enabled {
		if pf.Telegram.APIKey == "" || pf.Telegram.ChatID == "" {
			return nil, fmt.Errorf("telegram.enabled requires api_key and chat_id")
		}
	}
	return names, nil
}

// provisionHost runs the full init for one host with the fleet globals.
func provisionHost(pf ProvisionFile, h ProvisionHost) provisionResult {
	fw := h.FirewallAllowlist
	if fw == "" {
		fw = pf.FirewallAllowlist
	}
	adm := h.AdminIPs
	if adm == "" {
		adm = pf.AdminIPs
	}
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := infraFlags{
		user:              h.User,
		key:               h.SSHKey,
		port:              port,
		mode:              pf.Mode,
		firewallAllowlist: fw,
		adminIPs:          adm,
		noTraefik:         pf.NoTraefik,
	}
	fmt.Printf("\n━━━ Host %s (%s) ━━━\n", h.Name, h.Host)
	err := runInfraInitSSH(h.Host, f)
	return provisionResult{Name: h.Name, Host: h.Host, Error: err}
}

func findProvisionHost(hosts []ProvisionHost, name string) (ProvisionHost, error) {
	for _, h := range hosts {
		if h.Name == name {
			return h, nil
		}
	}
	return ProvisionHost{}, fmt.Errorf("host %q not found", name)
}
