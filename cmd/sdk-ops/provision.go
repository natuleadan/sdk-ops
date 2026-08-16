package main

import (
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/deploy"
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
	HTTPSMode         string                 `yaml:"https_mode"`
	NoTraefik         bool                   `yaml:"no_traefik"`
	// Hardening enables the OS hardening phase on first init. Default true;
	// `hardening: false` skips it (docker + services only — fast drills).
	Hardening *bool `yaml:"hardening,omitempty"`
	Groups            map[string]GroupConfig `yaml:"groups"`
	Hosts             []ProvisionHost        `yaml:"hosts"`
	Peers             []ProvisionPeer        `yaml:"peers"`
	Bans              []string               `yaml:"bans"`
	Telegram          TelegramConfig         `yaml:"telegram"`
	Security          SecurityConfig         `yaml:"security"`
	Fail2ban          Fail2banConfig         `yaml:"fail2ban"`
	SSL               SSLConfig              `yaml:"ssl"`
	Traefik           TraefikConfig          `yaml:"traefik"`
	Swap              SwapConfig             `yaml:"swap"`
	Services          ProvisionServices      `yaml:"services,omitempty"`
	VLANs             []ProvisionVLAN        `yaml:"vlans"`
	DeployOrder       []DeployStep           `yaml:"deploy_order"`
}

// ProvisionHost is a single VPS entry. Per-host values override the group
// and the globals.
type ProvisionHost struct {
	Name              string            `yaml:"name"`
	Host              string            `yaml:"host"`
	PeerIP            string            `yaml:"peer_ip,omitempty"`
	Group             string            `yaml:"group,omitempty"`
	User              string            `yaml:"user"`
	SSHKey            string            `yaml:"ssh_key"`
	Port              int               `yaml:"port"`
	FirewallAllowlist string            `yaml:"firewall_allowlist,omitempty"`
	AdminIPs          string            `yaml:"admin_ips,omitempty"`
	HTTPSMode         string            `yaml:"https_mode,omitempty"`
	SwapSizeMB        int               `yaml:"swap_size_mb,omitempty"`
	Security          *SecurityConfig   `yaml:"security,omitempty"`
	Traefik           *TraefikConfig    `yaml:"traefik,omitempty"`
	Services          ProvisionServices `yaml:"services,omitempty"`
	Tags              []string          `yaml:"tags,omitempty"`
}

// ProvisionServices declares which services run on a node and how they are
// sized. Each value picks a template profile (e.g. lite vs rs). Secrets are
// NOT stored here — they come from the environment/.env at provision time.
type ProvisionServices map[string]ServiceConfig

// ServiceConfig is one declared service on a host.
type ServiceConfig struct {
	Profile         string   `yaml:"profile"`
	// Replicas is the desired stream/bucket replica count (1 = single node,
	// 2 = R2, 3 = R3...). Validated against the cluster topology: it can be
	// satisfied either across N VPS nodes (N hosts with the service) or by a
	// single VPS running N containers. 0 = default (derived from node count).
	Replicas        int      `yaml:"replicas,omitempty"`
	// Seeds is how many explicit mesh routes a node renders (default 3). A
	// small fleet lists all peers; a large fleet lists a few seeds and gossip
	// discovers the rest. 0 = default (min(nodeCount, 3)).
	Seeds           int      `yaml:"seeds,omitempty"`
	ServerTags      []string `yaml:"server_tags,omitempty"`
	ClientAdvertise string   `yaml:"client_advertise,omitempty"`
	// Mode is the postgres deployment mode: "cluster" (default, n>=2 nodes)
	// or "single" (1 node — Patroni + local DCS, no quorum).
	Mode string `yaml:"mode,omitempty"`
	// BackupMode selects where the pgbackrest backup runs: "server" (default,
	// a dedicated backup node — backup-standby, the leader is not loaded) or
	// "leader" (the pooler node, simple).
	BackupMode string `yaml:"backup_mode,omitempty"`
	// Restore enables the idempotent DR: if the cluster is NOT operational at
	// provision time, restore from S3 (pgbackrest). false = never restore.
	Restore bool `yaml:"restore,omitempty"`
	// Pooler marks this host as the PgDog node (the app entry). Default: the
	// first host that declares the postgres service.
	Pooler bool `yaml:"pooler,omitempty"`
	// Recreate wipes the service state (containers + data + DCS keys + networks)
	// before deploying — a clean redeploy from scratch. Granular per service:
	// only the declared service is wiped. Idempotent (no flag = skip).
	Recreate bool `yaml:"recreate,omitempty"`
	// Backup is the YAML-driven backup schedule (defaults: full daily + incr hourly).
	Backup *BackupSchedule `yaml:"backup,omitempty"`
}

// BackupSchedule is the YAML-driven pgbackrest cadence.
type BackupSchedule struct {
	Full string `yaml:"full,omitempty"` // systemd calendar for the full backup (default: daily 00:15)
	Diff string `yaml:"diff,omitempty"` // systemd calendar for the differential (default: none)
	Incr string `yaml:"incr,omitempty"` // systemd calendar for the incremental (default: hourly)
}

// GroupConfig is a reusable per-group configuration that hosts inherit.
type GroupConfig struct {
	FirewallAllowlist string            `yaml:"firewall_allowlist"`
	AdminIPs          string            `yaml:"admin_ips"`
	HTTPSMode         string            `yaml:"https_mode"`
	Security          *SecurityConfig   `yaml:"security,omitempty"`
	Traefik           *TraefikConfig    `yaml:"traefik,omitempty"`
	Services          ProvisionServices `yaml:"services,omitempty"`
	Swap              *SwapConfig       `yaml:"swap,omitempty"`
	Telegram          *TelegramConfig   `yaml:"telegram,omitempty"`
	Tags              []string          `yaml:"tags,omitempty"`
}

// VLANHostAssign pins a host to a private VLAN interface with a static IP.
type VLANHostAssign struct {
	Name  string `yaml:"name"`
	Iface string `yaml:"iface"`
	IP    string `yaml:"ip"`
}

// ProvisionVLAN is an internal L2 network shared by a subset of hosts.
// Traffic between those hosts stays inside the provider VLAN; the firewall
// still filters by source IP (peers are declared per IP).
type ProvisionVLAN struct {
	Name  string           `yaml:"name"`
	CIDR  string           `yaml:"cidr"`
	Hosts []VLANHostAssign `yaml:"hosts"`
}

// SecurityConfig enables the SSH brute force notifier (every 5 min).
type SecurityConfig struct {
	Enabled   bool `yaml:"enabled"`
	Threshold int  `yaml:"threshold"`
}

// Fail2banConfig tunes the fail2ban jails owned by sdk-ops. The operator
// admin IPs are always added to ignoreip (shared-NAT safety).
type Fail2banConfig struct {
	SSHBantime      int `yaml:"sshd_bantime,omitempty"`
	RecidiveBantime int `yaml:"recidive_bantime,omitempty"`
	MaxRetry        int `yaml:"maxretry,omitempty"`
}

// SwapConfig controls the swap file (rule is automatic unless SizeMB is set).
type SwapConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	SizeMB  int   `yaml:"size_mb,omitempty"`
}

// SSLConfig carries the Let's Encrypt contact email for Traefik.
type SSLConfig struct {
	Email   string       `yaml:"email"`
	Staging bool         `yaml:"staging"`
	DNS01   *DNS01Config `yaml:"dns01,omitempty"`
}

// DNS01Config enables wildcard certificates through a DNS provider API
// (cloudflare or bunny). The token is stored in the fleet YAML (private).
type DNS01Config struct {
	Provider string `yaml:"provider"`
	APIToken string `yaml:"api_token"`
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
	// ContainerPort is the port the service listens on inside its container
	// (default 80). Only used when Traefik runs on a docker bridge network.
	ContainerPort int `yaml:"container_port,omitempty"`
	// Wildcard routes every single-level subdomain (needs ssl.dns01 for the
	// wildcard certificate).
	Wildcard bool `yaml:"wildcard,omitempty"`
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
	httpsMode         string
	security          SecurityConfig
	traefik           TraefikConfig
	swap              SwapConfig
	services          ProvisionServices
	tags              []string
}

type provisionResult struct {
	Name  string
	Host  string
	Error error
}

func newProvisionCmd() *cobra.Command {
	var tags string
	var check bool
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
          - { domain: "app.example.com", service: hello, port: 8088 }
  hosts:
    - name: edge-01
      host: 203.0.113.20
      group: edge
      user: root
      ssh_key: ~/.ssh/id_ed25519
  peers:
    - from: edge-02
      to: edge-01
      ports: [6000]
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
			if check {
				return runProvisionCheck(args[0], tags)
			}
			return runProvision(args[0], tags)
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "Only provision hosts with any of these tags (comma-separated)")
	cmd.Flags().BoolVar(&check, "check", false, "Dry-run: parse, resolve and render the services plan without touching any node")
	return cmd
}

// newApplyCmd — the declarative one-command entry: `sdk-ops apply <file.yaml>`
// provisions the whole fleet (init + hardening + services + DR) in one shot,
// like `kubectl apply` — idempotent (re-applying an unchanged fleet is a
// no-op). `-v` prints the per-step detail.
func newApplyCmd() *cobra.Command {
	var tags string
	var check bool
	cmd := &cobra.Command{
		Use:   "apply <file.yaml>",
		Short: "Provision a fleet from a YAML file (one declarative command)",
		Long: `The declarative entry: sdk-ops apply fleet.yaml provisions the whole
fleet (init + hardening + services + DR) in one shot — idempotent, the
re-apply of an unchanged fleet is a no-op. -v prints the per-step detail.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if v, _ := cmd.Flags().GetBool("verbose"); v {
				verboseMode = true
			}
			if check {
				return runProvisionCheck(args[0], tags)
			}
			return runProvision(args[0], tags)
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "Only provision hosts with any of these tags (comma-separated)")
	cmd.Flags().BoolVar(&check, "check", false, "Dry-run: parse, resolve and render the services plan without touching any node")
	cmd.Flags().BoolP("verbose", "v", false, "Verbose: print the per-step detail (wire scripts output)")
	return cmd
}

// verboseMode gates the per-step detail of the apply/provision flows.
var verboseMode bool

// verbosef prints the per-step detail only under `apply -v` / `provision -v`.
func verbosef(format string, args ...any) {
	if verboseMode {
		fmt.Printf("  · "+format+"\n", args...)
	}
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
	for t := range strings.SplitSeq(tags, ",") {
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
		httpsMode:         pf.HTTPSMode,
		security:          pf.Security,
		traefik:           pf.Traefik,
		swap:              pf.Swap,
		services:          cloneServices(pf.Services),
	}
	if g, ok := pf.Groups[h.Group]; ok {
		r.applyGroup(g)
	}
	r.applyHost(h)
	return r
}

// applyGroup overlays a group config onto the resolved host (host wins later).
func (r *resolvedHost) applyGroup(g GroupConfig) {
	if g.FirewallAllowlist != "" {
		r.firewallAllowlist = g.FirewallAllowlist
	}
	if g.AdminIPs != "" {
		r.adminIPs = g.AdminIPs
	}
	if g.HTTPSMode != "" {
		r.httpsMode = g.HTTPSMode
	}
	if g.Security != nil {
		r.security = *g.Security
	}
	if g.Traefik != nil {
		r.traefik = *g.Traefik
	}
	if g.Services != nil {
		r.services = mergeServices(r.services, g.Services)
	}
	if g.Swap != nil {
		r.swap = *g.Swap
	}
	r.tags = append(r.tags, g.Tags...)
}

// applyHost overlays the host config (highest precedence).
func (r *resolvedHost) applyHost(h ProvisionHost) {
	if h.FirewallAllowlist != "" {
		r.firewallAllowlist = h.FirewallAllowlist
	}
	if h.AdminIPs != "" {
		r.adminIPs = h.AdminIPs
	}
	if h.HTTPSMode != "" {
		r.httpsMode = h.HTTPSMode
	}
	if h.Security != nil {
		r.security = *h.Security
	}
	if h.Traefik != nil {
		r.traefik = *h.Traefik
	}
	if h.Services != nil {
		r.services = mergeServices(r.services, h.Services)
	}
	if h.SwapSizeMB > 0 {
		r.swap.SizeMB = h.SwapSizeMB
	}
	r.tags = append(r.tags, h.Tags...)
}

func cloneServices(s ProvisionServices) ProvisionServices {
	out := ProvisionServices{}
	maps.Copy(out, s)
	return out
}

// mergeServices overlays src onto dst (per key; the overlay wins).
func mergeServices(dst, src ProvisionServices) ProvisionServices {
	out := cloneServices(dst)
	maps.Copy(out, src)
	return out
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
		noHardening:       pf.Hardening != nil && !*pf.Hardening,
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
	// Try the post-init user (sdkops) first: hardened nodes block root login.
	// A fresh node has no sdkops user, so this returns false and the caller
	// falls back to the YAML user (root) for the first init.
	if f.user != "sdkops" {
		f2 := *f
		f2.user = "sdkops"
		if conn, err := infraConnect(h.Host, &f2); err == nil {
			defer closeConn(conn)
			out, _, err := ssh.Run(conn, `test -f /opt/sdk-ops/.version && echo yes || echo no`)
			return err == nil && strings.TrimSpace(out) == "yes"
		}
	}
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
	if err := validateHTTPSMode(pf); err != nil {
		return nil, err
	}
	if err := validateDeployOrder(pf, names); err != nil {
		return nil, err
	}
	if err := validateVLANs(pf); err != nil {
		return nil, err
	}
	if err := validateServiceReplicas(pf); err != nil {
		return nil, err
	}
	return names, nil
}

// validateServiceReplicas checks the service replicas declaration. Replicas
// are satisfied either across N VPS nodes (N hosts with the service) or by a
// single VPS running N containers — the render picks the mode. A replicas
// count above the number of NATS nodes (and above the single-VPS container
// cap) cannot be placed, so it is rejected early.
func validateServiceReplicas(pf *ProvisionFile) error {
	nodeCount := 0
	for _, h := range pf.Hosts {
		if _, ok := resolveHostConfig(pf, h).services["nats"]; ok {
			nodeCount++
		}
	}
	for _, h := range pf.Hosts {
		r := resolveHostConfig(pf, h)
		for name, svc := range r.services {
			if svc.Replicas < 0 {
				return fmt.Errorf("host %q service %q replicas must be >= 0 (0 = default)", h.Name, name)
			}
			if svc.Replicas <= 1 {
				continue
			}
			if nodeCount == 1 && svc.Replicas > 9 {
				return fmt.Errorf("host %q service %q replicas %d exceeds the single-VPS container cap (9)", h.Name, name, svc.Replicas)
			}
			if nodeCount > 1 && svc.Replicas > nodeCount {
				return fmt.Errorf("host %q service %q replicas %d exceeds the %d NATS nodes", h.Name, name, svc.Replicas, nodeCount)
			}
		}
	}
	return nil
}

// validateVLANs checks VLAN assignments: known hosts, non-empty iface, IP
// inside the VLAN CIDR and unique per host.
func validateVLANs(pf *ProvisionFile) error {
	hosts := map[string]bool{}
	for _, h := range pf.Hosts {
		hosts[h.Name] = true
	}
	seen := map[string]string{}
	for _, v := range pf.VLANs {
		if v.Name == "" {
			return fmt.Errorf("every vlan needs a name")
		}
		_, ipnet, err := net.ParseCIDR(v.CIDR)
		if err != nil {
			return fmt.Errorf("vlan %q cidr %q is not a valid CIDR: %v", v.Name, v.CIDR, err)
		}
		for _, a := range v.Hosts {
			if !hosts[a.Name] {
				return fmt.Errorf("vlan %q references unknown host %q", v.Name, a.Name)
			}
			if a.Iface == "" {
				return fmt.Errorf("vlan %q host %q needs an iface", v.Name, a.Name)
			}
			ip := net.ParseIP(strings.TrimSpace(a.IP))
			if ip == nil {
				return fmt.Errorf("vlan %q host %q ip %q is not a valid IP", v.Name, a.Name, a.IP)
			}
			if !ipnet.Contains(ip) {
				return fmt.Errorf("vlan %q host %q ip %q is outside %q", v.Name, a.Name, a.IP, v.CIDR)
			}
			key := a.Name + ":" + v.Name
			if prev, ok := seen[a.IP]; ok {
				return fmt.Errorf("vlan ip %q already assigned to %s (also on %s)", a.IP, prev, key)
			}
			seen[a.IP] = key
		}
	}
	return nil
}

// vlansForHost returns the VLAN assignments of one host.
func vlansForHost(pf ProvisionFile, hostName string) []ProvisionVLAN {
	var out []ProvisionVLAN
	for _, v := range pf.VLANs {
		for _, a := range v.Hosts {
			if a.Name == hostName {
				out = append(out, v)
				break
			}
		}
	}
	return out
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
	lists := []struct {
		domains []TraefikDomain
		where   string
	}{
		{pf.Traefik.Domains, "global"},
	}
	for name, g := range pf.Groups {
		if g.Traefik != nil {
			lists = append(lists, struct {
				domains []TraefikDomain
				where   string
			}{g.Traefik.Domains, "group " + name})
		}
	}
	for _, h := range pf.Hosts {
		if h.Traefik != nil {
			lists = append(lists, struct {
				domains []TraefikDomain
				where   string
			}{h.Traefik.Domains, "host " + h.Name})
		}
	}
	for _, l := range lists {
		for _, d := range l.domains {
			if err := validateTraefikDomain(d, l.where, pf.SSL.DNS01); err != nil {
				return err
			}
		}
	}
	return validateDNS01Token(pf.SSL.DNS01)
}

// validateDNS01Token ensures a declared DNS-01 provider has its token.
func validateDNS01Token(dns01 *DNS01Config) error {
	if dns01 != nil && dns01.Provider != "" && dns01.APIToken == "" {
		return fmt.Errorf("ssl.dns01 requires api_token")
	}
	return nil
}

// validateTraefikDomain checks one domain entry: it needs domain + port, and
// wildcard entries require a DNS-01 provider (wildcards cannot use HTTP).
func validateTraefikDomain(d TraefikDomain, where string, dns01 *DNS01Config) error {
	if d.Domain == "" || d.Port <= 0 {
		return fmt.Errorf("traefik domain needs domain and port (%s)", where)
	}
	if !d.Wildcard {
		return nil
	}
	if dns01 == nil || dns01.Provider == "" {
		return fmt.Errorf("traefik wildcard domain %q requires ssl.dns01 (wildcard certificates cannot be issued over HTTP)", d.Domain)
	}
	if dns01.Provider != "cloudflare" && dns01.Provider != "bunny" {
		return fmt.Errorf("ssl.dns01 provider %q not supported (cloudflare, bunny)", dns01.Provider)
	}
	return nil
}

// validateHTTPSMode checks the https_mode values (cf = web ports gated to the
// CDN allowlist, all = web ports open to every IP).
func validateHTTPSMode(pf *ProvisionFile) error {
	check := func(mode, where string) error {
		switch mode {
		case "", "cf", "all":
			return nil
		default:
			return fmt.Errorf("https_mode %q invalid (%s): use cf or all", mode, where)
		}
	}
	if err := check(pf.HTTPSMode, "global"); err != nil {
		return err
	}
	for name, g := range pf.Groups {
		if err := check(g.HTTPSMode, "group "+name); err != nil {
			return err
		}
	}
	for _, h := range pf.Hosts {
		if err := check(h.HTTPSMode, "host "+h.Name); err != nil {
			return err
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
// using each host's resolved (group-inherited) configuration. The FLEET-wide
// etcd (the DCS) deploys FIRST on every host — the postgres patroni needs the
// etcd QUORUM (2/3) before the leader election; a single member blocks the
// linearizable reads and the postgres wire would wait forever.
func applyPerHostPhases(pf ProvisionFile) error {
	if err := applyFleetEtcd(pf); err != nil {
		return err
	}
	for _, h := range pf.Hosts {
		if err := applyPerHostPhaseOn(pf, h); err != nil {
			return err
		}
	}
	return nil
}

// applyFleetEtcd deploys the etcd service on every host that declares it —
// the DCS quorum must exist BEFORE any postgres node starts (the per-host
// phase re-deploys it idempotently: an unchanged config is a no-op).
func applyFleetEtcd(pf ProvisionFile) error {
	for _, h := range pf.Hosts {
		cfg, ok := resolveHostConfig(&pf, h).services["etcd"]
		if !ok {
			continue
		}
		port := h.Port
		if port == 0 {
			port = 22
		}
		etcdUser := h.User
		if etcdUser == "" {
			etcdUser = "sdkops"
		}
		f := infraFlags{user: etcdUser, key: h.SSHKey, port: port, mode: pf.Mode, noHardening: pf.Hardening != nil && !*pf.Hardening}
		conn, err := infraConnect(h.Host, &f)
		if err != nil {
			return fmt.Errorf("fleet etcd: connect %s: %w", h.Name, err)
		}
		err = deployServiceOn(conn, pf, h, "etcd", cfg)
		closeConn(conn)
		if err != nil {
			return fmt.Errorf("fleet etcd %s: %w", h.Name, err)
		}
		fmt.Printf("→ fleet etcd up on %s\n", h.Name)
	}
	return nil
}

// hostInfraFlags builds the SSH flags for a host: the YAML user (root in the
// no-hardening mode) with the sdkops fallback for hardened nodes.
func hostInfraFlags(pf ProvisionFile, h ProvisionHost, port int) infraFlags {
	user := h.User
	if user == "" {
		user = "sdkops"
	}
	return infraFlags{
		user:        user,
		key:         h.SSHKey,
		port:        port,
		mode:        pf.Mode,
		noHardening: pf.Hardening != nil && !*pf.Hardening,
	}
}

// applyPerHostPhaseOn applies the per-host fleet phases for a single host:
// security watch, fail2ban jail, firewall state watchdog, logrotate, traefik
// watchdog, VLAN interface and Traefik domains.
func applyPerHostPhaseOn(pf ProvisionFile, h ProvisionHost) error {
	if err := applyWatchdogPhasesOn(pf, h); err != nil {
		return err
	}
	for _, v := range vlansForHost(pf, h.Name) {
		for _, a := range v.Hosts {
			if a.Name == h.Name {
				if err := applyVLANOn(pf, h, v.Name, a.Iface, a.IP, v.CIDR); err != nil {
					return err
				}
				break
			}
		}
	}
	if len(resolveHostConfig(&pf, h).traefik.Domains) > 0 {
		port := h.Port
		if port == 0 {
			port = 22
		}
		f := hostInfraFlags(pf, h, port)
		conn, err := infraConnect(h.Host, &f)
		if err != nil {
			return fmt.Errorf("traefik: connect %s: %w", h.Name, err)
		}
		r := resolveHostConfig(&pf, h)
		if err := applyTraefikDomainsOn(conn, pf.SSL, r.traefik, h.Name); err != nil {
			closeConn(conn)
			return err
		}
		closeConn(conn)
	}
	if err := applyServicesOn(pf, h); err != nil {
		return err
	}
	return nil
}

// applyWatchdogPhasesOn installs the cron/watchdog phases of one host:
// security watch, fail2ban jail, firewall state watchdog, logrotate and the
// traefik watchdog.
func applyWatchdogPhasesOn(pf ProvisionFile, h ProvisionHost) error {
	r := resolveHostConfig(&pf, h)
	if r.security.Enabled {
		if err := installSecurityOn(pf, h, r.security.Threshold); err != nil {
			return err
		}
	}
	if err := installFail2banJailOn(pf, h); err != nil {
		return err
	}
	if err := installStateWatchOn(pf, h); err != nil {
		return err
	}
	if err := installLogRotationOn(pf, h); err != nil {
		return err
	}
	if !pf.NoTraefik {
		if err := installTraefikWatchOn(pf, h); err != nil {
			return err
		}
	}
	return nil
}

// installStateWatchOn installs the firewall state watchdog on one host.
func installStateWatchOn(pf ProvisionFile, h ProvisionHost) error {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := hostInfraFlags(pf, h, port)
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("state watch: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)
	if err := hardening.InstallStateWatch(conn); err != nil {
		return err
	}
	return nil
}

// installTraefikWatchOn installs the reverse proxy watchdog on one host.
func installTraefikWatchOn(pf ProvisionFile, h ProvisionHost) error {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := hostInfraFlags(pf, h, port)
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("traefik watch: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)
	if err := hardening.InstallTraefikWatch(conn); err != nil {
		return err
	}
	return nil
}

// applyVLANOn brings the VLAN interface up, assigns the private IP (runtime,
// idempotent) and persists it through a netplan drop-in. netplan apply is
// never run live: the runtime address is enough and a wrong apply could drop
// the management SSH session.
func applyVLANOn(pf ProvisionFile, h ProvisionHost, vlanName, iface, ip, cidr string) error {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("vlan %q: %w", vlanName, err)
	}
	mask, _ := ipnet.Mask.Size()
	addr := ip + "/" + fmt.Sprintf("%d", mask)
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := hostInfraFlags(pf, h, port)
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("vlan %q: connect %s: %w", vlanName, h.Name, err)
	}
	defer closeConn(conn)
	conn2, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("vlan %q: connect %s: %w", vlanName, h.Name, err)
	}
	closeConn(conn2)
	script := fmt.Sprintf(`
set -e
if ! ip link show %s >/dev/null 2>&1; then
  echo "vlan %s: iface %s does not exist on %s" >&2
  exit 1
fi
sudo ip link set %s up 2>/dev/null || true
if ! ip -4 addr show dev %s 2>/dev/null | grep -q "%s"; then
  sudo ip addr replace %s dev %s
  echo "vlan %s: %s assigned on %s"
else
  echo "vlan %s: %s already on %s"
fi
NETPLAN=/etc/netplan/99-sdk-ops-vlan.yaml
TMP=$(mktemp)
cat > "$TMP" << NETPLANEOF
network:
  version: 2
  ethernets:
    %s:
      addresses:
        - %s
NETPLANEOF
sudo cp "$TMP" "$NETPLAN" && rm -f "$TMP"
sudo chmod 0600 "$NETPLAN"
echo "vlan %s: netplan drop-in persisted"
`, iface, vlanName, iface, h.Name, iface, iface, addr, addr, iface, vlanName, addr, iface, vlanName, addr, iface, iface, addr, vlanName)
	out, _, err := ssh.Run(conn, script)
	if err != nil {
		return fmt.Errorf("vlan %q on %s: %w\n%s", vlanName, h.Name, err, out)
	}
	fmt.Print(out)
	return nil
}

// installLogRotationOn installs the logrotate policy on one host.
func installLogRotationOn(pf ProvisionFile, h ProvisionHost) error {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := hostInfraFlags(pf, h, port)
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("logrotate: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)
	if err := hardening.InstallLogRotation(conn); err != nil {
		return err
	}
	return nil
}

// installFail2banJailOn applies the fail2ban jail.local owned by sdk-ops
// (idempotent: written and reloaded only when the content differs). The
// operator admin IPs from the fleet are always ignored by fail2ban.
func installFail2banJailOn(pf ProvisionFile, h ProvisionHost) error {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := hostInfraFlags(pf, h, port)
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("fail2ban: connect %s: %w", h.Name, err)
	}
	defer closeConn(conn)
	r := resolveHostConfig(&pf, h)
	cfg := hardening.Fail2banJailConfig{
		SSHBantime:      pf.Fail2ban.SSHBantime,
		RecidiveBantime: pf.Fail2ban.RecidiveBantime,
		MaxRetry:        pf.Fail2ban.MaxRetry,
	}
	for ip := range strings.SplitSeq(r.adminIPs, ",") {
		if ip = strings.TrimSpace(ip); ip != "" {
			cfg.IgnoreIPs = append(cfg.IgnoreIPs, hardening.NormalizeCIDR(ip))
		}
	}
	if err := hardening.InstallFail2banJail(conn, cfg); err != nil {
		return err
	}
	return nil
}

// installSecurityOn installs the brute force watcher on one host.
func installSecurityOn(pf ProvisionFile, h ProvisionHost, threshold int) error {
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := hostInfraFlags(pf, h, port)
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
// applyTraefikDomainsOn writes one router file per domain and the main
// Traefik config. Routers are picked up by the file provider (watch: true),
// so no restart is needed. The router target depends on how Traefik runs:
//   - host network (IPv6-only nodes): http://localhost:<published port>
//   - docker bridge: http://<service>:<container_port> via the sdk-ops-net
//     network (docker DNS), which the installer connects Traefik to.
//
// Wildcard domains (Wildcard: true) use the v3 HostRegexp rule and request a
// wildcard certificate, which requires ssl.dns01.
func applyTraefikDomainsOn(conn *golang_ssh.Client, ssl SSLConfig, traefik TraefikConfig, hostName string) error {
	out, _, err := ssh.Run(conn, `docker inspect traefik --format '{{.HostConfig.NetworkMode}}' 2>/dev/null || echo host`)
	if err != nil {
		return fmt.Errorf("traefik network mode: %w", err)
	}
	hostNet := strings.TrimSpace(out) == "host"

	// Bridge-mode Traefik reaches the 404 responder (traefik-404 on :18080)
	// through the docker gateway, so the docker subnets must reach that port.
	if !hostNet {
		if err := hardening.AllowlistExposePort(conn, 18080, "tcp", hardening.PortScopeIPs, "172.16.0.0/12"); err != nil { // go-check:ignore-ip (docker bridge CIDR)
			return fmt.Errorf("traefik 404 port 18080: %w", err)
		}
	}

	// Persist the container creation template the traefik watchdog re-runs
	// when the container is missing.
	if err := ensureTraefikTemplate(conn, ssl); err != nil {
		return err
	}

	for _, d := range traefik.Domains {
		rule, tlsBlock, target := traefikRouterConfig(d, hostNet)
		router := fmt.Sprintf(`http:
  routers:
    %[1]s:
      rule: "%[2]s"
      service: %[1]s
      entryPoints:
        - websecure
      %[3]s
  services:
    %[1]s:
      loadBalancer:
        servers:
          - url: "%[4]s"
`, routerName(d.Domain), rule, tlsBlock, target)
		script := fmt.Sprintf(`
sudo tee /etc/traefik/conf.d/%[1]s.yml > /dev/null << 'ROUTEREOF'
%[2]s
ROUTEREOF
`, strings.ReplaceAll(d.Domain, ".", "_"), router)
		if _, _, err := ssh.Run(conn, script); err != nil {
			return fmt.Errorf("traefik router %s: %w", d.Domain, err)
		}
		fmt.Printf("  → %s -> %s on %s\n", d.Domain, target, hostName)
	}

	caServer := "https://acme-v02.api.letsencrypt.org/directory"
	if ssl.Staging {
		caServer = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	challenge := "      httpChallenge:\n        entryPoint: web"
	if ssl.DNS01 != nil && ssl.DNS01.Provider != "" {
		challenge = fmt.Sprintf("      dnsChallenge:\n        provider: %s\n        resolvers:\n          - \"1.1.1.1:53\"\n          - \"8.8.8.8:53\"", ssl.DNS01.Provider) // go-check:ignore-ip (public DNS resolvers)
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
%[3]s
providers:
  file:
    directory: /etc/traefik/conf.d
    watch: true
EOF
echo "letsencrypt: configured (%[1]s)"`, ssl.Email, caServer, challenge)
	if _, _, err := ssh.Run(conn, script); err != nil {
		return fmt.Errorf("traefik letsencrypt: %w", err)
	}

	// Ensure the container carries the DNS provider token when DNS-01 is used.
	if ssl.DNS01 != nil && ssl.DNS01.APIToken != "" {
		envName := "CF_DNS_API_TOKEN"
		if ssl.DNS01.Provider == "bunny" {
			envName = "BUNNY_API_KEY"
		}
		if err := ensureTraefikEnv(conn, envName, ssl.DNS01.APIToken); err != nil {
			return fmt.Errorf("traefik dns01 env: %w", err)
		}
	}
	return nil
}

// routerName maps a domain to a safe router key (the domain itself, as used
// in the file provider YAML).
func routerName(domain string) string {
	return strings.ReplaceAll(domain, "*", "wildcard")
}

// dns01Env builds the Traefik container env for a DNS-01 provider token.
func dns01Env(ssl SSLConfig) []string {
	if ssl.DNS01 == nil || ssl.DNS01.APIToken == "" {
		return nil
	}
	name := "CF_DNS_API_TOKEN"
	if ssl.DNS01.Provider == "bunny" {
		name = "BUNNY_API_KEY"
	}
	return []string{name + "=" + ssl.DNS01.APIToken}
}

// ensureTraefikTemplate persists the container creation script that the
// traefik watchdog runs to recreate a vanished container.
func ensureTraefikTemplate(conn *golang_ssh.Client, ssl SSLConfig) error {
	script, err := deploy.TraefikCreateScript(conn, deploy.ProxyConfig{Env: dns01Env(ssl)})
	if err != nil {
		return fmt.Errorf("traefik template: %w", err)
	}
	remote := fmt.Sprintf(`sudo mkdir -p /opt/sdk-ops/traefik
cat > /tmp/traefik-install.sh << 'EOF'
%s
EOF
sudo cp /tmp/traefik-install.sh /opt/sdk-ops/traefik/install.sh
id sdkops >/dev/null 2>&1 && sudo chown sdkops:sdkops /opt/sdk-ops/traefik/install.sh
sudo chmod 0750 /opt/sdk-ops/traefik/install.sh
echo "traefik install template persisted"
`, script)
	out, _, err := ssh.Run(conn, remote)
	if err != nil {
		return fmt.Errorf("ensure traefik template: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

// traefikRouterConfig computes the rule, TLS block and backend target for one
// domain. Host-network Traefik reaches services via localhost; bridge-mode
// via the service name on the shared docker network (container_port).
func traefikRouterConfig(d TraefikDomain, hostNet bool) (rule, tlsBlock, target string) {
	target = fmt.Sprintf("http://localhost:%d", d.Port)
	if !hostNet {
		cp := d.ContainerPort
		if cp == 0 {
			cp = 80
		}
		target = fmt.Sprintf("http://%s:%d", d.Service, cp)
	}
	rule = fmt.Sprintf("Host(`%s`)", d.Domain)
	tlsBlock = "tls:\n        certResolver: letsencrypt"
	if d.Wildcard {
		apex := strings.TrimPrefix(d.Domain, "*.")
		rule = fmt.Sprintf("HostRegexp(`^[a-z0-9-]+\\.%s$`)", strings.ReplaceAll(apex, ".", `\\.`))
		tlsBlock = fmt.Sprintf("tls:\n        certResolver: letsencrypt\n        domains:\n          - main: %s\n            sans:\n              - \"*.%s\"", apex, apex)
	}
	return rule, tlsBlock, target
}

// ensureTraefikEnv recreates the Traefik container when the given env var is
// missing, so DNS-01 provider tokens reach the ACME resolver. Other config is
// untouched (docker restart policy keeps it running).
func ensureTraefikEnv(conn *golang_ssh.Client, envName, value string) error {
	script := fmt.Sprintf(`
if docker inspect traefik >/dev/null 2>&1 && ! docker inspect traefik --format '{{range .Config.Env}}{{.}}{{"\n"}}{{end}}' | grep -q '^%[1]s='; then
  echo "traefik: adding %[1]s (recreate)"
  CMD=$(docker inspect traefik --format '{{.Config.Image}}')
  NET=$(docker inspect traefik --format '{{.HostConfig.NetworkMode}}')
  ARGS=""
  if [ "$NET" = "host" ]; then ARGS="--network host"; else ARGS="-p 80:80 -p 443:443"; fi
  VOLS=$(docker inspect traefik --format '{{range .Mounts}}-v {{.Source}}:{{.Destination}}{{if .RW}}:ro{{end}} {{end}}' | sed 's/:ro :/: /g')
  sudo docker rm -f traefik >/dev/null 2>&1 || true
  sudo docker run -d --name traefik --restart unless-stopped $ARGS -e %[1]s='%[2]s' $VOLS $CMD >/dev/null
  echo "traefik: recreated with %[1]s"
else
  echo "traefik: env %[1]s already set"
fi
`, envName, value)
	out, _, err := ssh.Run(conn, script)
	if err != nil {
		return fmt.Errorf("ensure traefik env: %w\n%s", err, out)
	}
	fmt.Print(out)
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
	peerUser := toHost.User
	if peerUser == "" {
		peerUser = "sdkops"
	}
	f := infraFlags{
		user:        peerUser,
		key:         toHost.SSHKey,
		port:        port,
		mode:        pf.Mode,
		noHardening: pf.Hardening != nil && !*pf.Hardening,
	}
	conn, err := infraConnect(toHost.Host, &f)
	if err != nil {
		return fmt.Errorf("peer %s: connect %s: %w", peer.From, peer.To, err)
	}
	defer closeConn(conn)
	for _, p := range peer.Ports {
		if f.noHardening {
			// No firewall in the no-hardening mode — the mesh is open.
			fmt.Printf("  → %s can reach %s:%d (no firewall)\n", peer.From, peer.To, p)
			continue
		}
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
		f := hostInfraFlags(pf, h, port)
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
		f := hostInfraFlags(pf, h, port)
		conn, err := infraConnect(h.Host, &f)
		if err != nil {
			return fmt.Errorf("telegram: connect %s: %w", h.Name, err)
		}
		script := fmt.Sprintf(`sudo tee /etc/sdk-ops/firewall/notify.env > /dev/null << 'EOF'
export TELEGRAM_API_KEY=%s
export TELEGRAM_CHAT_ID=%s
EOF
EOF
sudo chown sdkops:sdkops /etc/sdk-ops/firewall/notify.env 2>/dev/null || true
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
