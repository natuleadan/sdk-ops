package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/ssh"
)

// ProvisionFile is the fleet YAML: N hosts, each inheriting configuration
// from an optional group, with peers, bans, telegram, security, ssl and
// traefik sections. Precedence: host override > group > global.
type ProvisionFile struct {
	Mode              string                 `yaml:"mode"`
	Parallel          int                    `yaml:"parallel"`
	FirewallAllowlist string                 `yaml:"firewall_allowlist"`
	AdminIPs          string                 `yaml:"admin_ips"`
	NoTraefik         bool                   `yaml:"no_traefik"`
	Groups            map[string]GroupConfig `yaml:"groups"`
	Hosts             []ProvisionHost        `yaml:"hosts"`
	Peers             []ProvisionPeer        `yaml:"peers"`
	Bans              []string               `yaml:"bans"`
	Telegram          TelegramConfig         `yaml:"telegram"`
	Security          SecurityConfig         `yaml:"security"`
	SSL               SSLConfig              `yaml:"ssl"`
	Traefik           TraefikConfig          `yaml:"traefik"`
	Swap              SwapConfig             `yaml:"swap"`
	DeployOrder       []DeployStep           `yaml:"deploy_order"`
}

// ProvisionHost is a single VPS entry. Per-host values override the group
// and the globals.
type ProvisionHost struct {
	Name              string          `yaml:"name"`
	Host              string          `yaml:"host"`
	PeerIP            string          `yaml:"peer_ip,omitempty"`
	Group             string          `yaml:"group,omitempty"`
	User              string          `yaml:"user"`
	SSHKey            string          `yaml:"ssh_key"`
	Port              int             `yaml:"port"`
	FirewallAllowlist string          `yaml:"firewall_allowlist,omitempty"`
	AdminIPs          string          `yaml:"admin_ips,omitempty"`
	SwapSizeMB        int             `yaml:"swap_size_mb,omitempty"`
	Security          *SecurityConfig `yaml:"security,omitempty"`
	Tags              []string        `yaml:"tags,omitempty"`
}

// GroupConfig is a reusable per-group configuration that hosts inherit.
type GroupConfig struct {
	FirewallAllowlist string          `yaml:"firewall_allowlist"`
	AdminIPs          string          `yaml:"admin_ips"`
	Security          *SecurityConfig `yaml:"security,omitempty"`
	Traefik           *TraefikConfig  `yaml:"traefik,omitempty"`
	Swap              *SwapConfig     `yaml:"swap,omitempty"`
	Telegram          *TelegramConfig `yaml:"telegram,omitempty"`
	Tags              []string        `yaml:"tags,omitempty"`
}

// SecurityConfig enables the SSH brute force notifier (every 5 min).
type SecurityConfig struct {
	Enabled   bool `yaml:"enabled"`
	Threshold int  `yaml:"threshold"`
}

// SwapConfig controls the swap file (rule is automatic unless SizeMB is set).
type SwapConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	SizeMB  int   `yaml:"size_mb,omitempty"`
}

// SSLConfig carries the Let's Encrypt contact email for Traefik.
type SSLConfig struct {
	Email   string `yaml:"email"`
	Staging bool   `yaml:"staging"`
}

// TraefikConfig maps domains to local services for auto TLS.
type TraefikConfig struct {
	Enabled bool            `yaml:"enabled"`
	Domains []TraefikDomain `yaml:"domains"`
}

// TraefikDomain routes a domain to a service on a local port.
type TraefikDomain struct {
	Domain  string `yaml:"domain"`
	Service string `yaml:"service"`
	Port    int    `yaml:"port"`
}

// TelegramConfig enables Telegram alerts for security/refresh events.
type TelegramConfig struct {
	Enabled bool   `yaml:"enabled"`
	APIKey  string `yaml:"api_key"`
	ChatID  string `yaml:"chat_id"`
}

// ProvisionPeer opens ports on host To restricted to host From's IP.
type ProvisionPeer struct {
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Ports []int  `yaml:"ports"`
}

// DeployStep orders deployment: either a group (all its hosts) or one host.
type DeployStep struct {
	Group string `yaml:"group"`
	Host  string `yaml:"host"`
}

// resolvedHost is the effective configuration of a host after merging
// global + group + host overrides.
type resolvedHost struct {
	host              ProvisionHost
	firewallAllowlist string
	adminIPs          string
	security          SecurityConfig
	traefik           TraefikConfig
	swap              SwapConfig
	tags              []string
}

type provisionResult struct {
	Name  string
	Host  string
	Error error
}

func newProvisionCmd() *cobra.Command {
	var tags string
	cmd := &cobra.Command{
		Use:   "provision <file.yaml>",
		Short: "Provision multiple VPSes from a YAML fleet file",
		Long: `Provision multiple VPSes from a single YAML file.

Each host runs the full init (hardening + swap + Docker + Traefik + optional
provider allowlist) in parallel, inheriting configuration from its group
(precedence: host > group > global). The peers/bans/telegram/security/ssl
sections are applied per host. Use --tags to operate on a subset.

Example provision.yaml:
  mode: docker
  parallel: 3
  firewall_allowlist: cf
  admin_ips: "203.0.113.10,2001:db8::1"
  groups:
    edge:
      security: { enabled: true, threshold: 5 }
      traefik:
        enabled: true
        domains:
          - { domain: "test.nla.run", service: hello, port: 8088 }
  hosts:
    - name: netcup
      host: 152.53.169.115
      group: edge
      user: root
      ssh_key: ~/.ssh/id_ed25519
  peers:
    - from: vps-lite
      to: netcup
      ports: [43453]
  bans:
    - 198.51.100.7
  telegram:
    enabled: true
    api_key: "123456:ABC..."
    chat_id: "-1001234567890"
  ssl:
    email: "admin@example.com"
  deploy_order:
    - group: edge
    - host: some-host`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProvision(args[0], tags)
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "Only provision hosts with any of these tags (comma-separated)")
	return cmd
}

func runProvision(path, tags string) error {
	data, err := os.ReadFile(filepath.Clean(path))
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
	names, err := validateProvision(&pf)
	if err != nil {
		return err
	}

	hosts := selectHostsByTags(pf.Hosts, tags)
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts match the given tags")
	}
	parallel := pf.Parallel
	if parallel <= 0 {
		parallel = 1
	}

	fmt.Printf("→ Provisioning %d hosts (mode=%s, parallel=%d)\n", len(hosts), pf.Mode, parallel)
	results := provisionHosts(pf, hosts, parallel)
	failed := countProvisionFailures(results)
	if failed > 0 {
		return fmt.Errorf("%d/%d hosts failed to provision", failed, len(results))
	}

	if err := applyProvisionPhases(pf, names); err != nil {
		return err
	}

	fmt.Println("\n✅ Provision complete")
	return nil
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

// selectHostsByTags filters hosts by any matching tag (empty tags = all).
func selectHostsByTags(hosts []ProvisionHost, tags string) []ProvisionHost {
	if strings.TrimSpace(tags) == "" {
		return hosts
	}
	want := map[string]bool{}
	for _, t := range strings.Split(tags, ",") {
		want[strings.TrimSpace(t)] = true
	}
	var out []ProvisionHost
	for _, h := range hosts {
		if hostMatchesTags(h, want) {
			out = append(out, h)
		}
	}
	return out
}

func hostMatchesTags(h ProvisionHost, want map[string]bool) bool {
	for _, t := range h.Tags {
		if want[t] {
			return true
		}
	}
	return false
}

// resolveHostConfig merges global + group + host for a single host.
func resolveHostConfig(pf *ProvisionFile, h ProvisionHost) resolvedHost {
	r := resolvedHost{
		host:              h,
		firewallAllowlist: pf.FirewallAllowlist,
		adminIPs:          pf.AdminIPs,
		security:          pf.Security,
		traefik:           pf.Traefik,
		swap:              pf.Swap,
	}
	if g, ok := pf.Groups[h.Group]; ok {
		if g.FirewallAllowlist != "" {
			r.firewallAllowlist = g.FirewallAllowlist
		}
		if g.AdminIPs != "" {
			r.adminIPs = g.AdminIPs
		}
		if g.Security != nil {
			r.security = *g.Security
		}
		if g.Traefik != nil {
			r.traefik = *g.Traefik
		}
		if g.Swap != nil {
			r.swap = *g.Swap
		}
		r.tags = append(r.tags, g.Tags...)
	}
	if h.FirewallAllowlist != "" {
		r.firewallAllowlist = h.FirewallAllowlist
	}
	if h.AdminIPs != "" {
		r.adminIPs = h.AdminIPs
	}
	if h.Security != nil {
		r.security = *h.Security
	}
	if h.SwapSizeMB > 0 {
		r.swap.SizeMB = h.SwapSizeMB
	}
	r.tags = append(r.tags, h.Tags...)
	return r
}

// provisionHost runs the full init for one host with its resolved config.
func provisionHost(pf ProvisionFile, h ProvisionHost) provisionResult {
	r := resolveHostConfig(&pf, h)
	fw := r.firewallAllowlist
	adm := r.adminIPs
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
	fmt.Printf("\n━━━ Host %s (%s) [group=%s] ━━━\n", h.Name, h.Host, h.Group)
	already := hostAlreadyInitialized(h, &f)
	if !already {
		err := runInfraInitSSH(h.Host, f)
		if err != nil {
			return provisionResult{Name: h.Name, Host: h.Host, Error: err}
		}
	} else {
		fmt.Println("  → already initialized, applying phases only")
	}

	// Swap override per host (when explicitly sized).
	if r.swap.SizeMB > 0 {
		conn, err := infraConnect(h.Host, &f)
		if err != nil {
			return provisionResult{Name: h.Name, Host: h.Host, Error: err}
		}
		defer closeConn(conn)
		if err := applySwapSize(conn, r.swap.SizeMB); err != nil {
			return provisionResult{Name: h.Name, Host: h.Host, Error: err}
		}
	}
	return provisionResult{Name: h.Name, Host: h.Host}
}

// hostAlreadyInitialized reports whether the node carries the sdk-ops init
// marker, in which case provisioning skips the full init and only applies
// the fleet phases (fast re-provision).
func hostAlreadyInitialized(h ProvisionHost, f *infraFlags) bool {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f.port = port
	conn, err := infraConnect(h.Host, f)
	if err != nil {
		return false
	}
	defer closeConn(conn)
	out, _, err := ssh.Run(conn, `test -f /opt/sdk-ops/.version && echo yes || echo no`)
	return err == nil && strings.TrimSpace(out) == "yes"
}

// applySwapSize resizes the swap file to an explicit size in MB.
func applySwapSize(conn *golang_ssh.Client, sizeMB int) error {
	script := fmt.Sprintf(`
CUR_MB=0
[ -f /swapfile ] && CUR_MB=$(( $(stat -c%%s /swapfile) / 1024 / 1024 ))
if [ "$CUR_MB" -ne %d ]; then
  sudo swapoff /swapfile 2>/dev/null || true
  sudo rm -f /swapfile
  sudo fallocate -l %dM /swapfile 2>/dev/null || sudo dd if=/dev/zero of=/swapfile bs=1M count=%d 2>/dev/null
  sudo chmod 600 /swapfile
  sudo mkswap /swapfile 2>/dev/null
fi
sudo swapon /swapfile 2>/dev/null || true
sudo grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab > /dev/null
echo "swap: OK (%dM override)"
`, sizeMB, sizeMB, sizeMB, sizeMB)
	_, _, err := ssh.Run(conn, script)
	if err != nil {
		return fmt.Errorf("swap override: %w", err)
	}
	return nil
}

// provisionHosts runs host inits honoring deploy_order when present
// (each step is a parallel wave), otherwise all hosts at once.
func provisionHosts(pf ProvisionFile, hosts []ProvisionHost, parallel int) []provisionResult {
	if len(pf.DeployOrder) == 0 {
		return provisionWave(pf, hosts, parallel)
	}

	byName := map[string]ProvisionHost{}
	for _, h := range hosts {
		byName[h.Name] = h
	}
	var results []provisionResult
	done := map[string]bool{}
	for _, step := range pf.DeployOrder {
		var wave []ProvisionHost
		if step.Group != "" {
			for _, h := range hosts {
				if h.Group == step.Group && !done[h.Name] {
					wave = append(wave, h)
					done[h.Name] = true
				}
			}
		} else if step.Host != "" {
			if h, ok := byName[step.Host]; ok && !done[h.Name] {
				wave = append(wave, h)
				done[h.Name] = true
			}
		}
		if len(wave) > 0 {
			fmt.Printf("→ Deploy wave: %s\n", waveNames(wave))
			results = append(results, provisionWave(pf, wave, parallel)...)
		}
	}
	return results
}

func waveNames(hosts []ProvisionHost) string {
	var names []string
	for _, h := range hosts {
		names = append(names, h.Name)
	}
	return strings.Join(names, ", ")
}

func provisionWave(pf ProvisionFile, hosts []ProvisionHost, parallel int) []provisionResult {
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	results := make([]provisionResult, len(hosts))
	for i, h := range hosts {
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

// validateProvision validates the fleet file and returns name->host IP map
// (peer_ip fallback host).
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
		if h.Group != "" {
			if _, ok := pf.Groups[h.Group]; !ok {
				return nil, fmt.Errorf("host %q references unknown group %q", h.Name, h.Group)
			}
		}
	}
	if err := validatePeers(pf, names); err != nil {
		return nil, err
	}
	if err := validateBansAndTelegram(pf); err != nil {
		return nil, err
	}
	if err := validateTraefikDomains(pf); err != nil {
		return nil, err
	}
	if err := validateDeployOrder(pf, names); err != nil {
		return nil, err
	}
	return names, nil
}

// validatePeers checks peer references and ports.
func validatePeers(pf *ProvisionFile, names map[string]string) error {
	for _, peer := range pf.Peers {
		if names[peer.From] == "" {
			return fmt.Errorf("peer from %q not found in hosts", peer.From)
		}
		if names[peer.To] == "" {
			return fmt.Errorf("peer to %q not found in hosts", peer.To)
		}
		if len(peer.Ports) == 0 {
			return fmt.Errorf("peer %s -> %s has no ports", peer.From, peer.To)
		}
	}
	return nil
}

// validateDeployOrder checks deploy_order references.
func validateDeployOrder(pf *ProvisionFile, names map[string]string) error {
	for _, step := range pf.DeployOrder {
		switch {
		case step.Group != "":
			if _, ok := pf.Groups[step.Group]; !ok {
				return fmt.Errorf("deploy_order references unknown group %q", step.Group)
			}
		case step.Host != "":
			if names[step.Host] == "" {
				return fmt.Errorf("deploy_order references unknown host %q", step.Host)
			}
		default:
			return fmt.Errorf("deploy_order step needs group or host")
		}
	}
	return nil
}

// validateBansAndTelegram checks the bans and telegram sections.
func validateBansAndTelegram(pf *ProvisionFile) error {
	for _, b := range pf.Bans {
		if _, err := hardening.ValidateCIDR(strings.TrimSpace(b)); err != nil {
			return fmt.Errorf("bans entry %q is not a valid IP/CIDR: %v", b, err)
		}
	}
	if pf.Telegram.Enabled {
		if pf.Telegram.APIKey == "" || pf.Telegram.ChatID == "" {
			return fmt.Errorf("telegram.enabled requires api_key and chat_id")
		}
	}
	return nil
}

// validateTraefikDomains checks the traefik section requirements.
func validateTraefikDomains(pf *ProvisionFile) error {
	if len(pf.Traefik.Domains) == 0 && !hasAnyTraefikDomains(pf) {
		return nil
	}
	if pf.SSL.Email == "" {
		return fmt.Errorf("traefik domains require ssl.email (Let's Encrypt contact)")
	}
	for _, d := range pf.Traefik.Domains {
		if d.Domain == "" || d.Port <= 0 {
			return fmt.Errorf("traefik domain needs domain and port")
		}
	}
	for _, g := range pf.Groups {
		if g.Traefik != nil {
			for _, d := range g.Traefik.Domains {
				if d.Domain == "" || d.Port <= 0 {
					return fmt.Errorf("traefik domain needs domain and port")
				}
			}
		}
	}
	return nil
}

func hasAnyTraefikDomains(pf *ProvisionFile) bool {
	if len(pf.Traefik.Domains) > 0 {
		return true
	}
	for _, g := range pf.Groups {
		if g.Traefik != nil && len(g.Traefik.Domains) > 0 {
			return true
		}
	}
	return false
}

// applyProvisionPhases runs peers, bans, telegram, security and per-host
// traefik/security phases after all hosts are initialized.
func applyProvisionPhases(pf ProvisionFile, names map[string]string) error {
	if len(pf.Peers) > 0 {
		fmt.Println("\n→ Configuring peers...")
		for _, peer := range pf.Peers {
			if err := applyProvisionPeer(pf, names, peer); err != nil {
				return err
			}
		}
		fmt.Println("→ Peers configured")
	}
	if len(pf.Bans) > 0 {
		if err := applyProvisionBans(pf); err != nil {
			return err
		}
	}
	if pf.Telegram.Enabled {
		if err := applyTelegramNotify(pf); err != nil {
			return err
		}
	}

	if err := applyPerHostPhases(pf); err != nil {
		return err
	}
	fmt.Println("→ Host phases applied")
	return nil
}

// applyPerHostPhases installs security watch and traefik domains per host
// using each host's resolved (group-inherited) configuration.
func applyPerHostPhases(pf ProvisionFile) error {
	for _, h := range pf.Hosts {
		r := resolveHostConfig(&pf, h)
		if r.security.Enabled {
			if err := installSecurityOn(pf, h, r.security.Threshold); err != nil {
				return err
			}
		}
		if len(r.traefik.Domains) > 0 {
			port := h.Port
			if port == 0 {
				port = 22
			}
			f := infraFlags{user: "sdkops", key: h.SSHKey, port: port, mode: pf.Mode}
			conn, err := infraConnect(h.Host, &f)
			if err != nil {
				return fmt.Errorf("traefik: connect %s: %w", h.Name, err)
			}
			if err := applyTraefikDomainsOn(conn, pf.SSL, r.traefik, h.Name); err != nil {
				closeConn(conn)
				return err
			}
			closeConn(conn)
		}
	}
	return nil
}

// installSecurityOn installs the brute force watcher on one host.
func installSecurityOn(pf ProvisionFile, h ProvisionHost, threshold int) error {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := infraFlags{user: "sdkops", key: h.SSHKey, port: port, mode: pf.Mode}
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("security: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)
	if err := hardening.InstallSecurityWatch(conn, hardening.SecurityWatchConfig{
		Enabled:   true,
		Threshold: threshold,
	}); err != nil {
		return err
	}
	return nil
}

// applyTraefikDomainsOn creates Traefik routers (Let's Encrypt) per domain
// on one host, opening port 80 to the LE challenge ranges.
func applyTraefikDomainsOn(conn *golang_ssh.Client, ssl SSLConfig, traefik TraefikConfig, hostName string) error {
	if err := hardening.AllowlistExposePort(conn, 80, "tcp", hardening.PortScopeIPs, "91.198.159.0/24", "51.89.149.0/24"); err != nil {
		return fmt.Errorf("traefik letsencrypt port 80: %w", err)
	}
	for _, d := range traefik.Domains {
		router := fmt.Sprintf(`http:
  routers:
    %[1]s:
      rule: "Host(\x60%[1]s\x60)"
      service: %[1]s
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
  services:
    %[1]s:
      loadBalancer:
        servers:
          - url: "http://localhost:%[2]d"
`, d.Domain, d.Port)
		script := fmt.Sprintf(`
sudo tee /etc/traefik/conf.d/%[1]s.yml > /dev/null << 'ROUTEREOF'
%[2]s
ROUTEREOF
`, strings.ReplaceAll(d.Domain, ".", "_"), router)
		if _, _, err := ssh.Run(conn, script); err != nil {
			return fmt.Errorf("traefik router %s: %w", d.Domain, err)
		}
		fmt.Printf("  → %s -> localhost:%d on %s\n", d.Domain, d.Port, hostName)
	}

	caServer := "https://acme-v02.api.letsencrypt.org/directory"
	if ssl.Staging {
		caServer = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	script := fmt.Sprintf(`sudo tee /etc/traefik/traefik.yml > /dev/null << 'EOF'
global:
  sendAnonymousUsage: false
api:
  dashboard: false
entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
certificatesResolvers:
  letsencrypt:
    acme:
      email: %[1]s
      storage: /opt/traefik/acme.json
      caServer: %[2]s
      httpChallenge:
        entryPoint: web
providers:
  file:
    directory: /etc/traefik/conf.d
    watch: true
EOF
sudo docker restart traefik 2>/dev/null || true
echo "letsencrypt: configured (%[1]s)"`, ssl.Email, caServer)
	if _, _, err := ssh.Run(conn, script); err != nil {
		return fmt.Errorf("traefik letsencrypt: %w", err)
	}
	return nil
}

// applyProvisionPeer opens the peer ports on the target host for the source
// host IP (scope ips). The fleet file is the single source of truth for peers.
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
	for _, p := range peer.Ports {
		_ = hardening.AllowlistUnexposePort(conn, p)
		fmt.Printf("  → %s can reach %s:%d\n", peer.From, peer.To, p)
		if err := hardening.AllowlistExposePort(conn, p, "tcp", hardening.PortScopeIPs, fromIP); err != nil {
			return fmt.Errorf("peer %s -> %s port %d: %w", peer.From, peer.To, p, err)
		}
	}
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

func findProvisionHost(hosts []ProvisionHost, name string) (ProvisionHost, error) {
	for _, h := range hosts {
		if h.Name == name {
			return h, nil
		}
	}
	return ProvisionHost{}, fmt.Errorf("host %q not found", name)
}
