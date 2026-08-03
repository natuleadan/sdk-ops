package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"
	golang_ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/docker"
	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/hooks"
	"github.com/natuleadan/sdk-ops/k3s"
	"github.com/natuleadan/sdk-ops/providers"
	"github.com/natuleadan/sdk-ops/providers/aws"
	"github.com/natuleadan/sdk-ops/providers/civo"
	"github.com/natuleadan/sdk-ops/providers/cubepath"
	"github.com/natuleadan/sdk-ops/providers/digitalocean"
	"github.com/natuleadan/sdk-ops/providers/hetzner"
	"github.com/natuleadan/sdk-ops/providers/vultr"
	"github.com/natuleadan/sdk-ops/ssh"
)

type infraFlags struct {
	user              string
	key               string
	port              int
	mode              string // k3s, docker, bare
	crowdsec          bool
	airgap            bool
	monitor           bool
	auditd            bool
	lynis             bool
	usg               bool
	lockRoot          bool
	hardSSHPort       int
	logsURL           string
	alertsURL         string
	firewallAllowlist string
	adminIPs          string
	noTraefik         bool
	provisionYAML     string
	// k3s-specific
	disableTraefik        bool
	secretsEncryption     bool
	protectKernelDefaults bool
	admissionPlugins      string
	cisPSA                bool
	cisAuditLog           bool
	cisNetPol             bool
	cisSvcAcc             bool
	cisTLSCiphers         bool
	kubeconfig            string
	mergeConfig           bool
	contextName           string
	// provider-specific
	provider  string
	plan      string
	location  string
	template  string
	hostname  string
	sshKeyIDs string
	apiKey    string
	projectID int
}

func newInfraCmd() *cobra.Command {
	var f infraFlags

	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Provision and manage VPS infrastructure",
	}

	cmd.PersistentFlags().StringVarP(&f.user, "user", "u", "root", "SSH user")
	cmd.PersistentFlags().StringVarP(&f.key, "key", "k", "", "SSH private key path (default: ~/.ssh/id_ed25519)")
	cmd.PersistentFlags().IntVarP(&f.port, "port", "p", 22, "SSH port")

	cmd.AddCommand(newInfraInitCmd(&f))
	cmd.AddCommand(newInfraJoinCmd(&f))
	cmd.AddCommand(newInfraStatusCmd(&f))
	cmd.AddCommand(newInfraReadyCmd(&f))
	cmd.AddCommand(newInfraAdoptCmd(&f))
	cmd.AddCommand(newInfraRemoveCmd(&f))
	cmd.AddCommand(newInfraFirewallCmd(&f))
	cmd.AddCommand(newSwapCmd(&f))
	cmd.AddCommand(newInfraUninstallCmd(&f))
	cmd.AddCommand(newInfraBackupCmd(&f))
	cmd.AddCommand(newInfraRestoreCmd(&f))
	cmd.AddCommand(newInfraCertCmd(&f))
	cmd.AddCommand(newInfraLogsCmd(&f))
	cmd.AddCommand(newInfraAlertsCmd(&f))
	cmd.AddCommand(newProvisionCmd())
	cmd.AddCommand(proxyCmd)

	return cmd
}

func newInfraInitCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [ip]",
		Short: "Initialize a VPS from zero: harden + install",
		Long: `Initialize a fresh VPS with security hardening and optional software.

With an IP: provision via SSH (traditional).
With --provider: create a VPS via API, then provision via SSH.

  --k3s      Install Docker + k3s (default)
  --docker   Install Docker only (no k3s)
  --bare     Only harden the OS (no Docker, no k3s)

  --crowdsec      Install CrowdSec WAF/IPS (suggested)
  --disable-traefik  Disable Traefik ingress in k3s

Provider options:
  --provider      Provider name (cubepath, hetzner, digitalocean, vultr, aws)
  --plan          VPS plan
  --location      Location
  --template      OS template
  --ssh-key-ids   Comma-separated SSH key IDs
  --api-key       API key for the provider
  --project-id    Project ID for the provider

Examples:
  sdk-ops infra init 188.xxx.xxx.xxx
  sdk-ops infra init --provider cubepath --plan gp.nano --location us-mia-1
  sdk-ops infra init --provider vultr --plan vc2-1c-2gb --location ewr
  sdk-ops infra init 188.xxx.xxx.xxx --docker --crowdsec`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			ip := ""
			if len(args) > 0 {
				ip = args[0]
			}
			return runInfraInit(ip, *f)
		},
	}

	cmd.Flags().StringVar(&f.mode, "mode", "k3s", "Installation mode: k3s, docker, bare")
	cmd.Flags().BoolVar(&f.crowdsec, "crowdsec", false, "Install CrowdSec (WAF/IPS)")
	cmd.Flags().BoolVar(&f.monitor, "monitor", false, "Install Prometheus node_exporter (port 9100)")
	cmd.Flags().BoolVar(&f.auditd, "auditd", false, "Install auditd for system auditing (CIS)")
	cmd.Flags().BoolVar(&f.lynis, "lynis", false, "Install Lynis security auditor")
	cmd.Flags().BoolVar(&f.usg, "usg", false, "Install Ubuntu Security Guide (CIS Level 1/2 auditing)")
	cmd.Flags().BoolVar(&f.lockRoot, "lock-root", false, "Lock root password after creating sdkops user")
	cmd.Flags().IntVar(&f.hardSSHPort, "ssh-port", 0, "Migrate SSH to custom port (0=keep port 22)")
	cmd.Flags().StringVar(&f.logsURL, "logs", "", "Install Promtail and ship logs to this Loki URL")
	cmd.Flags().StringVar(&f.alertsURL, "alerts", "", "Install Alertmanager with this Slack webhook URL")
	cmd.Flags().StringVar(&f.firewallAllowlist, "firewall-allowlist", "", "Install provider IP allowlist after hardening: cf, url:<url>, dns:<fqdn>, or strict / strict:<source>")
	cmd.Flags().StringVar(&f.adminIPs, "admin-ips", "", "Explicit admin IPs/CIDRs for the allowlist (comma-separated, v4 and v6). No auto-seeding")
	cmd.Flags().BoolVar(&f.noTraefik, "no-traefik", false, "Do not install Traefik as the default reverse proxy (catch-all 404)")
	cmd.Flags().StringVar(&f.provisionYAML, "provision-yaml", "", "Fleet YAML to finish the init with per-host phases (security/state watch, VLANs, Traefik domains)")
	cmd.Flags().BoolVar(&f.disableTraefik, "disable-traefik", false, "Disable Traefik ingress in k3s")
	cmd.Flags().BoolVar(&f.secretsEncryption, "secrets-encryption", false, "Enable secrets encryption at rest in etcd (CIS)")
	cmd.Flags().BoolVar(&f.protectKernelDefaults, "protect-kernel-defaults", false, "Protect kubelet kernel defaults (CIS)")
	cmd.Flags().StringVar(&f.admissionPlugins, "admission-plugins", "NodeRestriction,EventRateLimit", "Kube-apiserver admission plugins (CIS)")
	cmd.Flags().BoolVar(&f.cisPSA, "cis-psa", false, "Enforce Pod Security Admission restricted (CIS)")
	cmd.Flags().BoolVar(&f.cisAuditLog, "cis-audit-log", false, "Enable kube-apiserver audit logging (CIS)")
	cmd.Flags().BoolVar(&f.cisNetPol, "cis-netpol", false, "Apply default-deny NetworkPolicy (CIS)")
	cmd.Flags().BoolVar(&f.cisSvcAcc, "cis-svcacc", false, "Patch default ServiceAccount automount=false (CIS)")
	cmd.Flags().BoolVar(&f.cisTLSCiphers, "cis-tls-ciphers", false, "Restrict TLS cipher suites (CIS)")
	cmd.Flags().StringVar(&f.kubeconfig, "kubeconfig", "./kubeconfig", "Path to save kubeconfig")
	cmd.Flags().BoolVar(&f.mergeConfig, "merge", false, "Merge kubeconfig into ~/.kube/config")
	cmd.Flags().StringVar(&f.contextName, "context", "sdk-ops-cluster", "Kubeconfig context name")

	cmd.Flags().Bool("k3s", false, "Install Docker + k3s")
	cmd.Flags().Bool("docker", false, "Install Docker only")
	cmd.Flags().Bool("bare", false, "Only harden the OS")

	cmd.Flags().StringVar(&f.provider, "provider", "", "Create VPS via provider (cubepath, hetzner, digitalocean, vultr, aws)")
	cmd.Flags().StringVar(&f.plan, "plan", "", "VPS plan")
	cmd.Flags().StringVar(&f.location, "location", "", "VPS location")
	cmd.Flags().StringVar(&f.template, "template", "", "OS template")
	cmd.Flags().StringVar(&f.hostname, "hostname", "", "VPS hostname")
	cmd.Flags().StringVar(&f.sshKeyIDs, "ssh-key-ids", "", "SSH key IDs (comma-separated)")
	cmd.Flags().StringVar(&f.apiKey, "api-key", "", "Provider API key (or provider-specific env var)")
	cmd.Flags().IntVar(&f.projectID, "project-id", 0, "Provider project ID")
	cmd.Flags().BoolVar(&f.airgap, "airgap", false, "Pre-download k3s binary and copy via SSH (no internet on target)")

	cmd.PreRunE = func(cobraCmd *cobra.Command, args []string) error {
		useK3s, _ := cobraCmd.Flags().GetBool("k3s")
		useDocker, _ := cobraCmd.Flags().GetBool("docker")
		useBare, _ := cobraCmd.Flags().GetBool("bare")
		switch {
		case useK3s:
			f.mode = "k3s"
		case useDocker:
			f.mode = "docker"
		case useBare:
			f.mode = "bare"
		}
		return nil
	}

	return cmd
}

func newInfraJoinCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join <server-ip> <agent-ip>",
		Short: "Join a worker node to a k3s cluster",
		Long: `Join a worker/agent node to an existing k3s cluster.

  --server-user   SSH user for the server (default: same as --user)
  --token         Cluster token (auto-fetched if SSH access to server)

Examples:
  sdk-ops infra join 188.xxx.xxx.100 188.xxx.xxx.101
  sdk-ops infra join 188.xxx.xxx.100 188.xxx.xxx.101 --token mytoken`,
		Args: cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			serverUser, _ := cobraCmd.Flags().GetString("server-user")
			token, _ := cobraCmd.Flags().GetString("token")
			return runInfraJoin(args[0], args[1], serverUser, token, *f)
		},
	}

	cmd.Flags().String("server-user", "", "SSH user for the server (default: same as --user)")
	cmd.Flags().String("token", "", "Cluster token (auto-fetched if SSH access to server)")

	return cmd
}

func newInfraStatusCmd(f *infraFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status <ip>",
		Short: "Show server health and installed components",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runInfraStatus(args[0], *f)
		},
	}
}

func newInfraReadyCmd(f *infraFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "ready <ip>",
		Short: "Check if a node's cluster is fully operational",
		Long: `Check if k3s is installed, running, and all nodes are Ready.
Exits with code 0 if ready, 1 otherwise.

Examples:
  sdk-ops infra ready 188.xxx.xxx.xxx
  sdk-ops infra ready 188.xxx.xxx.xxx --context my-cluster`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runInfraReady(args[0], *f)
		},
	}
}

func newInfraAdoptCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adopt <ip>",
		Short: "Scan an existing server and register it without reprovisioning",
		Long: `Connect to a server, detect what's already installed (Docker, k3s,
services, databases), and register it in the sdk-ops config.

Does NOT install anything — just scans and registers.

Examples:
  sdk-ops infra adopt 188.xxx.xxx.xxx
  sdk-ops infra adopt 188.xxx.xxx.xxx --mode docker
  sdk-ops infra adopt 188.xxx.xxx.xxx --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			forced, _ := cobraCmd.Flags().GetBool("force")
			adoptMode, _ := cobraCmd.Flags().GetString("mode")
			return runInfraAdopt(args[0], *f, forced, adoptMode)
		},
	}

	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	cmd.Flags().String("mode", "", "Override detected mode (k3s, docker, bare)")

	return cmd
}

func newInfraRemoveCmd(f *infraFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <ip>",
		Short: "Remove sdk-ops from a server (uninstall k3s/Docker)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runInfraRemove(args[0], *f)
		},
	}
}

func newInfraFirewallCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "firewall",
		Short: "Manage firewall rules on a node",
	}

	cmd.AddCommand(newInfraFirewallOpenCmd(f))
	cmd.AddCommand(newInfraFirewallCloseCmd(f))
	cmd.AddCommand(newInfraFirewallListCmd(f))
	cmd.AddCommand(newInfraFirewallCfNormalCmd(f))
	cmd.AddCommand(newInfraFirewallCfStrictCmd(f))
	cmd.AddCommand(newInfraFirewallAllowlistCmd(f))
	cmd.AddCommand(newInfraFirewallBanCmd(f))
	cmd.AddCommand(newInfraFirewallUnbanCmd(f))
	cmd.AddCommand(newInfraFirewallBansCmd(f))

	return cmd
}

// ban/unban/bans: explicit IP handling via the node's fail2ban jail. IPs are
// always literal — no auto-detection.
func newInfraFirewallBanCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ban <ip>",
		Short: "Ban an IP via fail2ban (explicit IP, no auto-detection)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				return hardening.Fail2banBan(conn, args[0])
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newInfraFirewallUnbanCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unban <ip>",
		Short: "Unban an IP via fail2ban (explicit IP, no auto-detection)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				return hardening.Fail2banUnban(conn, args[0])
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newInfraFirewallBansCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bans",
		Short: "List IPs banned by fail2ban on the node",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				out, err := hardening.Fail2banBans(conn)
				if err != nil {
					return err
				}
				fmt.Print(out)
				return nil
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newInfraFirewallOpenCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <port>[,<port>,...]",
		Short: "Open ports in the firewall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			ports, err := parsePorts(args[0])
			if err != nil {
				return err
			}
			proto, _ := cobraCmd.Flags().GetString("proto")
			node := firewalledNode(cobraCmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			for _, p := range ports {
				fmt.Printf("→ Opening port %d/%s on %s...\n", p, proto, node)
				if err := hardening.FirewallOpen(conn, p, proto); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().StringP("proto", "P", "tcp", "Protocol (tcp, udp)")
	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func parsePorts(raw string) ([]int, error) {
	var ports []int
	for s := range strings.SplitSeq(raw, ",") {
		p := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &p); err != nil {
			return nil, fmt.Errorf("invalid port: %q", s)
		}
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("port out of range: %d", p)
		}
		ports = append(ports, p)
	}
	return ports, nil
}

func newInfraFirewallCloseCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close <port>[,<port>,...]",
		Short: "Close ports in the firewall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			ports, err := parsePorts(args[0])
			if err != nil {
				return err
			}
			proto, _ := cobraCmd.Flags().GetString("proto")
			node := firewalledNode(cobraCmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			for _, p := range ports {
				fmt.Printf("→ Closing port %d/%s on %s...\n", p, proto, node)
				if err := hardening.FirewallClose(conn, p, proto); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().StringP("proto", "P", "tcp", "Protocol (tcp, udp)")
	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraFirewallListCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List firewall rules",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			node := firewalledNode(cobraCmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			out, err := hardening.FirewallList(conn)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}

	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

// allowlistFlags holds the shared flags for cf-normal / cf-strict.
type allowlistFlags struct {
	node          string
	source        string
	cloudFirewall string
	yes           bool
	adminIPs      string
	openWeb       bool
}

func addAllowlistFlags(cmd *cobra.Command, aw *allowlistFlags, strict bool) {
	cmd.Flags().StringVarP(&aw.node, "node", "n", "", "Target node IP")
	cmd.Flags().StringVar(&aw.source, "source", "cf", "IP list source: cf, url:<url>, dns:<fqdn>")
	cmd.Flags().StringVar(&aw.cloudFirewall, "cloud-firewall", "", "Also sync the allowlist to a provider cloud firewall (vultr, digitalocean, hetzner, ...)")
	cmd.Flags().StringVar(&aw.adminIPs, "admin-ips", "", "Explicit admin IPs/CIDRs (comma-separated, v4 and v6). No auto-seeding: pass your IPs explicitly to get permanent access")
	cmd.Flags().BoolVar(&aw.openWeb, "open-web", false, "Open ports 80/443 to every IP (DNS-only hosts not fronted by a CDN). Default: gated to the allowlist (Cloudflare)")
	if strict {
		cmd.Flags().BoolVar(&aw.yes, "yes", false, "Confirm the lockout risk warning and proceed")
	}
}

// parseAdminIPs splits a comma-separated IP/CIDR list into v4 and v6 entries.
func parseAdminIPs(raw string) (admin4, admin6 []string, err error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil, nil
	}
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		ip := hardening.NormalizeCIDR(strings.TrimSpace(part))
		if _, verr := hardening.ValidateCIDR(ip); verr != nil {
			return nil, nil, fmt.Errorf("admin IP %q: %w", part, verr)
		}
		if isV6, _ := hardening.ValidateCIDR(ip); isV6 {
			admin6 = append(admin6, ip)
		} else {
			admin4 = append(admin4, ip)
		}
	}
	return admin4, admin6, nil
}

func newInfraFirewallCfNormalCmd(f *infraFlags) *cobra.Command {
	var aw allowlistFlags
	cmd := &cobra.Command{
		Use:   "cf-normal",
		Short: "Restrict all inbound traffic to provider IPs, except SSH",
		Long: `Restrict ALL inbound traffic to the provider allowlist (Cloudflare by
default), keeping only SSH open from any IP.

  --source cf              Cloudflare ranges (default)
  --source url:<url>       Plain-text CIDR list (one per line)
  --source dns:<fqdn>      DNS TXT records with include: chains (Google style)

Installs a systemd timer on the node that refreshes the ranges daily.
Pass --admin-ips "ip1,ip2" to grant explicit IPs permanent access
(no auto-seeding of the operator IP).`,
		Args: cobra.NoArgs,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistInstall(f, hardening.AllowlistNormal, aw)
		},
	}
	addAllowlistFlags(cmd, &aw, false)
	return cmd
}

func newInfraFirewallCfStrictCmd(f *infraFlags) *cobra.Command {
	var aw allowlistFlags
	cmd := &cobra.Command{
		Use:   "cf-strict",
		Short: "Restrict ALL inbound traffic to provider IPs, including SSH",
		Long: `Restrict ALL inbound traffic, INCLUDING SSH, to the provider allowlist
(Cloudflare by default).

WARNING: you can be locked out if your public IP changes or the refresh
fails. This command requires --yes AND --admin-ips with at least one IP
(the install aborts without explicit admin IPs).`,
		Args: cobra.NoArgs,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistInstall(f, hardening.AllowlistStrict, aw)
		},
	}
	addAllowlistFlags(cmd, &aw, true)
	return cmd
}

// printAdminSummary shows which admin IPs were seeded.
func printAdminSummary(raw string, admin4, admin6 []string) {
	if len(admin4)+len(admin6) == 0 {
		fmt.Println("  → No admin IPs seeded (pass --admin-ips to grant yourself permanent access)")
		return
	}
	fmt.Printf("→ Admin IPs: %s\n", raw)
}

func runAllowlistInstall(f *infraFlags, profile hardening.AllowlistProfile, aw allowlistFlags) error {
	ctx := context.Background()
	if aw.node == "" {
		return fmt.Errorf("no node specified. Use --node or register one")
	}

	if profile == hardening.AllowlistStrict && !aw.yes {
		return fmt.Errorf(`cf-strict restricts ALL inbound traffic, INCLUDING SSH, to the allowlist. You can be locked out if your public IP changes or the refresh fails. Re-run with --yes to confirm`)
	}

	admin4, admin6, err := parseAdminIPs(aw.adminIPs)
	if err != nil {
		return err
	}
	if profile == hardening.AllowlistStrict && len(admin4)+len(admin6) == 0 {
		return fmt.Errorf(`cf-strict requires explicit admin IPs (--admin-ips "ip1,ip2") so you are never locked out`)
	}

	conn, err := infraConnect(aw.node, f)
	if err != nil {
		return err
	}
	defer closeConn(conn)

	src, err := hardening.ParseSource(aw.source)
	if err != nil {
		return err
	}

	v4, v6, err := hardening.FetchCIDRs(ctx, src)
	if err != nil {
		return err
	}
	if len(v4)+len(v6) < 4 {
		return fmt.Errorf("source %q returned too few ranges (%d v4, %d v6)", aw.source, len(v4), len(v6))
	}
	fmt.Printf("→ Source %s: %d IPv4 + %d IPv6 ranges\n", aw.source, len(v4), len(v6))

	cfg := hardening.AllowlistConfig{
		Profile:  profile,
		SSHPorts: hardening.CurrentSSHPorts(conn, f.port),
		Source:   src,
		Admin4:   admin4,
		Admin6:   admin6,
		OpenWeb:  aw.openWeb,
	}
	printAdminSummary(aw.adminIPs, admin4, admin6)

	fmt.Printf("→ Installing allowlist on %s...\n", aw.node)
	if err := installAndVerifyAllowlist(conn, aw.node, f, cfg); err != nil {
		return err
	}

	if aw.cloudFirewall != "" {
		if err := syncCloudFirewall(ctx, aw.cloudFirewall, v4, v6); err != nil {
			return err
		}
	}

	out, err := hardening.AllowlistStatus(conn)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// parseAllowlistFlag parses --firewall-allowlist values: a plain source
// (cf, url:..., dns:...) installs the normal profile; "strict" or
// "strict:<source>" installs the strict profile.
func parseAllowlistFlag(raw string) (hardening.AllowlistProfile, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "strict" {
		return hardening.AllowlistStrict, "cf", nil
	}
	if strings.HasPrefix(raw, "strict:") {
		src := strings.TrimPrefix(raw, "strict:")
		if _, err := hardening.ParseSource(src); err != nil {
			return hardening.AllowlistNormal, "", fmt.Errorf("--firewall-allowlist: %w", err)
		}
		return hardening.AllowlistStrict, src, nil
	}
	if _, err := hardening.ParseSource(raw); err != nil {
		return hardening.AllowlistNormal, "", fmt.Errorf("--firewall-allowlist: %w", err)
	}
	return hardening.AllowlistNormal, raw, nil
}

// createSDKOpsStructure creates the /opt/sdk-ops/ layout on the node.
func createSDKOpsStructure(conn *golang_ssh.Client) {
	if _, _, err := ssh.Run(conn, `sudo mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs && echo "sdk-ops-init" | sudo tee /opt/sdk-ops/.version > /dev/null`); err != nil {
		log.Printf("infra: ssh run error: %v", err)
	}
}

// registerInitNode saves the node in ~/.sdk-ops/config.yaml after init.
func registerInitNode(ip string, f infraFlags, hardCfg hardening.Config, arch string) {
	cfg, _ := loadConfig()
	found := false
	for i, n := range cfg.Nodes {
		if n.IP == ip {
			cfg.Nodes[i].Role = "server"
			cfg.Nodes[i].Arch = arch
			found = true
			break
		}
	}
	if !found {
		cfg.Nodes = append(cfg.Nodes, NodeConfig{
			IP:   ip,
			User: hardCfg.User,
			Key:  f.key,
			Port: hardCfg.SSHPort,
			Mode: f.mode,
			Role: "server",
			Arch: arch,
		})
		if err := saveConfig(cfg); err != nil {
			log.Printf("infra: save config error: %v", err)
		}
		fmt.Printf("  → Registered node in %s\n", configPath())
	} else if err := saveConfig(cfg); err != nil {
		log.Printf("infra: save config error: %v", err)
	}
}

// installTraefikPhase installs Docker (if missing) and Traefik with the
// catch-all 404 as the node's default reverse proxy.
func installTraefikPhase(conn *golang_ssh.Client) error {
	fmt.Println("\n  → Installing Traefik (default reverse proxy, catch-all 404)...")
	if err := docker.Install(conn); err != nil {
		return fmt.Errorf("traefik needs docker: %w", err)
	}
	if err := docker.EnsureNetworking(conn); err != nil {
		return err
	}
	if err := deploy.NewProxy(deploy.ProxyTraefik).Install(conn, deploy.ProxyConfig{}); err != nil {
		return err
	}
	return nil
}

// installAllowlistPhase runs the provider allowlist install requested via
// --firewall-allowlist at the end of infra init.
func installAllowlistPhase(conn *golang_ssh.Client, ip string, f infraFlags) error {
	profile, sourceRaw, err := parseAllowlistFlag(f.firewallAllowlist)
	if err != nil {
		return err
	}
	if profile == hardening.AllowlistStrict {
		fmt.Println("\n  → Installing strict provider allowlist (all ports, including SSH)...")
	} else {
		fmt.Println("\n  → Installing provider allowlist (all ports except SSH)...")
	}
	if err := installAllowlistOnNode(conn, ip, &f, profile, sourceRaw); err != nil {
		return fmt.Errorf("allowlist: %w", err)
	}
	return nil
}

// installAllowlistOnNode runs the full allowlist install (fetch, seed admin,
// apply, verify, commit) on an existing SSH connection.
func installAllowlistOnNode(conn *golang_ssh.Client, node string, f *infraFlags, profile hardening.AllowlistProfile, sourceRaw string) error {
	ctx := context.Background()
	src, err := hardening.ParseSource(sourceRaw)
	if err != nil {
		return err
	}
	v4, v6, err := hardening.FetchCIDRs(ctx, src)
	if err != nil {
		return err
	}
	if len(v4)+len(v6) < 4 {
		return fmt.Errorf("source %q returned too few ranges (%d v4, %d v6)", sourceRaw, len(v4), len(v6))
	}
	fmt.Printf("  → Source %s: %d IPv4 + %d IPv6 ranges\n", sourceRaw, len(v4), len(v6))

	admin4, admin6, err := parseAdminIPs(f.adminIPs)
	if err != nil {
		return err
	}
	if profile == hardening.AllowlistStrict && len(admin4)+len(admin6) == 0 {
		return fmt.Errorf(`strict allowlist requires explicit admin IPs (admin_ips in the provision YAML or --admin-ips)`)
	}

	cfg := hardening.AllowlistConfig{
		Profile:  profile,
		SSHPorts: hardening.CurrentSSHPorts(conn, f.port),
		Source:   src,
		Admin4:   admin4,
		Admin6:   admin6,
	}
	if len(admin4)+len(admin6) == 0 {
		fmt.Println("  → No admin IPs seeded (set admin_ips in the provision YAML to grant permanent access)")
	} else {
		fmt.Printf("  → Admin IPs: %s\n", f.adminIPs)
	}
	return installAndVerifyAllowlist(conn, node, f, cfg)
}

// installAndVerifyAllowlist applies the config, then opens a NEW SSH connection
// before trusting the new firewall. On verification failure the node-side
// auto-rollback restores the previous state automatically.
func installAndVerifyAllowlist(conn *golang_ssh.Client, node string, f *infraFlags, cfg hardening.AllowlistConfig) error {
	if err := hardening.InstallAllowlist(conn, cfg); err != nil {
		return err
	}
	fmt.Printf("→ Verifying with a new SSH connection...\n")
	start := time.Now()
	verifier := infraSSHClient(node, f.user, f.port, *f)
	verifierConn, err := verifier.Connect()
	if err != nil {
		return fmt.Errorf("new SSH connection failed after %s: %w\n  The node will restore the previous firewall automatically within 30s", time.Since(start).Round(time.Second), err)
	}
	defer closeConn(verifierConn)
	if err := hardening.CommitAllowlist(verifierConn); err != nil {
		return err
	}
	fmt.Printf("→ Verified and committed (new connection OK)\n")
	return nil
}

// detectAdminIP resolves the operator public IP for the permanent admin entry.
// infraConnect opens an SSH session to a node.
func infraConnect(node string, f *infraFlags) (*golang_ssh.Client, error) {
	client := infraSSHClient(node, f.user, f.port, *f)
	conn, err := client.Connect()
	if err != nil {
		return nil, fmt.Errorf("ssh: %w", err)
	}
	return conn, nil
}

func closeConn(conn *golang_ssh.Client) {
	if err := conn.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
	}
}

// syncCloudFirewall pushes the allowlist to a provider cloud firewall when the
// provider supports it. Providers without support are skipped with a warning.
func syncCloudFirewall(ctx context.Context, providerName string, v4, v6 []string) error {
	p, err := getInfraProvider(providerName, "", "", 0)
	if err != nil {
		return err
	}
	cfw, ok := p.(providers.CloudFirewall)
	if !ok {
		fmt.Printf("→ Cloud firewall: provider %q does not support allowlist sync, skipping\n", providerName)
		return nil
	}
	fmt.Printf("→ Syncing %d IPv4 + %d IPv6 ranges to %s cloud firewall...\n", len(v4), len(v6), providerName)
	if err := cfw.SyncFirewallAllowlist(ctx, v4, v6); err != nil {
		return err
	}
	fmt.Printf("→ %s cloud firewall synced\n", providerName)
	return nil
}

func newInfraFirewallAllowlistCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allowlist",
		Short: "Manage the provider IP allowlist firewall",
	}
	cmd.AddCommand(newAllowlistRefreshCmd(f))
	cmd.AddCommand(newAllowlistStatusCmd(f))
	cmd.AddCommand(newAllowlistRemoveCmd(f))
	cmd.AddCommand(newAllowlistAdminCmd(f))
	cmd.AddCommand(newAllowlistExposeCmd(f))
	cmd.AddCommand(newAllowlistUnexposeCmd(f))
	cmd.AddCommand(newAllowlistPortsCmd(f))
	cmd.AddCommand(newAllowlistPeerCmd(f))
	return cmd
}

// newAllowlistPeerCmd: peer add <ip> --ports "a,b" (sugar over expose) and
// peers (readable map of the ports registry).
func newAllowlistPeerCmd(f *infraFlags) *cobra.Command {
	peer := &cobra.Command{
		Use:   "peer",
		Short: "Manage peer-to-peer port access (who can reach which ports)",
	}
	var ports string
	var proto string
	add := &cobra.Command{
		Use:   "add <ip>",
		Short: "Allow a peer IP to reach specific ports (one rule per port)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			portList, err := parsePorts(ports)
			if err != nil {
				return err
			}
			if len(portList) == 0 {
				return fmt.Errorf("--ports is required (e.g. \"43453,3434\")")
			}
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				for _, p := range portList {
					if err := hardening.AllowlistExposePort(conn, p, proto, hardening.PortScopeIPs, args[0]); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	add.Flags().StringVar(&ports, "ports", "", "Comma-separated ports to open for this peer")
	add.Flags().StringVarP(&proto, "proto", "P", "tcp", "Protocol (tcp, udp)")
	add.Flags().StringP("node", "n", "", "Target node IP")

	list := &cobra.Command{
		Use:   "peers",
		Short: "Show the peer/port access map (ports registry)",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				out, err := hardening.AllowlistPorts(conn)
				if err != nil {
					return err
				}
				fmt.Println("port proto scope ips")
				for _, line := range strings.Split(out, "\n") {
					fields := strings.Fields(line)
					if len(fields) >= 3 {
						ipPart := ""
						if len(fields) > 3 && fields[2] == string(hardening.PortScopeIPs) {
							ipPart = fields[3]
						}
						fmt.Printf("%-6s %-5s %-7s %s\n", fields[0], fields[1], fields[2], ipPart)
					}
				}
				return nil
			})
		},
	}
	list.Flags().StringP("node", "n", "", "Target node IP")

	peer.AddCommand(add)
	peer.AddCommand(list)
	return peer
}

func newAllowlistRefreshCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Fetch provider ranges and apply them now",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, hardening.RefreshAllowlist)
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newAllowlistStatusCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show last sync state and live allowlist sets",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				out, err := hardening.AllowlistStatus(conn)
				if err != nil {
					return err
				}
				fmt.Print(out)
				return nil
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newAllowlistRemoveCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Restore the pre-allowlist firewall and remove the refresh timer",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, hardening.RemoveAllowlist)
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newAllowlistAdminCmd(f *infraFlags) *cobra.Command {
	admin := &cobra.Command{
		Use:   "admin",
		Short: "Manage permanent admin IPs in the allowlist",
	}
	admin.AddCommand(newAllowlistAdminModCmd(f, true))
	admin.AddCommand(newAllowlistAdminModCmd(f, false))
	return admin
}

func newAllowlistExposeCmd(f *infraFlags) *cobra.Command {
	var scope string
	var global bool
	var proto string
	var ips string
	cmd := &cobra.Command{
		Use:   "expose <port>",
		Short: "Expose a port (default: only admin IPs; --global: all IPs; --ips: explicit IP list; --scope traefik: register only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			ports, err := parsePorts(args[0])
			if err != nil {
				return err
			}
			if global && ips != "" {
				return fmt.Errorf("--global and --ips are mutually exclusive")
			}
			effectiveScope := hardening.PortScope(scope)
			var ipList []string
			if global {
				effectiveScope = hardening.PortScopeGlobal
			}
			if ips != "" {
				effectiveScope = hardening.PortScopeIPs
				for _, ip := range strings.Split(ips, ",") {
					ipList = append(ipList, strings.TrimSpace(ip))
				}
			}
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				if err := precheckNode(conn, firewalledNode(cobraCmd), f, false); err != nil {
					return err
				}
				return hardening.AllowlistExposePort(conn, ports[0], proto, effectiveScope, ipList...)
			})
		},
	}
	cmd.Flags().StringVarP(&scope, "scope", "s", string(hardening.PortScopeAdmin), "Scope: admin (default), global, ips, traefik")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Open to all IPs (shorthand for --scope global)")
	cmd.Flags().StringVar(&ips, "ips", "", "Explicit IP/CIDR list (comma-separated, v4 and v6) — shorthand for --scope ips")
	cmd.Flags().StringVarP(&proto, "proto", "P", "tcp", "Protocol (tcp, udp)")
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newAllowlistUnexposeCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unexpose <port>",
		Short: "Close an exposed port",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			ports, err := parsePorts(args[0])
			if err != nil {
				return err
			}
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				return hardening.AllowlistUnexposePort(conn, ports[0])
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newAllowlistPortsCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ports",
		Short: "List exposed ports and their scope",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				out, err := hardening.AllowlistPorts(conn)
				if err != nil {
					return err
				}
				fmt.Print(out)
				return nil
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

func newAllowlistAdminModCmd(f *infraFlags, add bool) *cobra.Command {
	use, short := "remove <ip>", "Remove a permanent admin IP"
	if add {
		use, short = "add <ip>", "Add a permanent admin IP"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return runAllowlistSimple(f, cobraCmd, func(conn *golang_ssh.Client) error {
				if add {
					return hardening.AllowlistAdminAdd(conn, args[0])
				}
				return hardening.AllowlistAdminRemove(conn, args[0])
			})
		},
	}
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	return cmd
}

// runAllowlistSimple wires a single-node allowlist operation.
func runAllowlistSimple(f *infraFlags, cobraCmd *cobra.Command, fn func(*golang_ssh.Client) error) error {
	node := firewalledNode(cobraCmd)
	if node == "" {
		return fmt.Errorf("no node specified. Use --node or register one")
	}
	conn, err := infraConnect(node, f)
	if err != nil {
		return err
	}
	defer closeConn(conn)
	return fn(conn)
}

func newInfraBackupCmd(f *infraFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "backup <ip>",
		Short: "Backup all services from a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			client := infraSSHClient(args[0], f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			path, err := deploy.BackupServices(conn, ".")
			if err != nil {
				return err
			}
			fmt.Printf("✅ Backup: %s\n", path)
			return nil
		},
	}
}

func newInfraRestoreCmd(f *infraFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <ip> <backup-file>",
		Short: "Restore services from a backup file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			client := infraSSHClient(args[0], f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			if err := deploy.RestoreServices(conn, args[1]); err != nil {
				return err
			}
			fmt.Println("✅ Restore complete")
			return nil
		},
	}
}

func newInfraCertCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage TLS certificates via Caddy",
	}

	cmd.AddCommand(newInfraCertInstallCmd(f))
	cmd.AddCommand(newInfraCertInfoCmd(f))

	return cmd
}

func newInfraCertInstallCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Caddy and provision TLS cert for a domain",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			domain, _ := cobraCmd.Flags().GetString("domain")
			email, _ := cobraCmd.Flags().GetString("email")
			port, _ := cobraCmd.Flags().GetInt("port")
			staging, _ := cobraCmd.Flags().GetBool("staging")
			node, _ := cobraCmd.Flags().GetString("node")

			if domain == "" {
				return fmt.Errorf("--domain is required")
			}
			if email == "" {
				return fmt.Errorf("--email is required")
			}
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}

			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()

			provider, _ := cobraCmd.Flags().GetString("provider")
			certFile, _ := cobraCmd.Flags().GetString("cert-file")
			keyFile, _ := cobraCmd.Flags().GetString("key-file")
			certRuntime, _ := cobraCmd.Flags().GetString("runtime")

			certProvider := deploy.CertLetsEncrypt
			switch provider {
			case "cloudflare":
				certProvider = deploy.CertCloudflare
			case "manual":
				certProvider = deploy.CertManual
			}

			return deploy.InstallCert(conn, deploy.CertConfig{
				Domain:     domain,
				Email:      email,
				Provider:   certProvider,
				CertFile:   certFile,
				KeyFile:    keyFile,
				TargetPort: port,
				Staging:    staging,
				Runtime:    certRuntime,
			})
		},
	}

	cmd.Flags().String("domain", "", "Domain to provision TLS for")
	cmd.Flags().String("email", "", "Email for Let's Encrypt")
	cmd.Flags().Int("port", 8080, "Local port to proxy")
	cmd.Flags().Bool("staging", false, "Use Let's Encrypt staging environment")
	cmd.Flags().StringP("node", "n", "", "Target node IP")
	cmd.Flags().String("provider", "letsencrypt", "Cert provider: letsencrypt, cloudflare, manual")
	cmd.Flags().String("cert-file", "", "Path to cert file (for --provider manual)")
	cmd.Flags().String("key-file", "", "Path to key file (for --provider manual)")
	cmd.Flags().String("runtime", "k3s", "Runtime: docker or k3s (affects how cert is installed)")

	return cmd
}

func newInfraCertInfoCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show TLS cert info for a domain",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			domain, _ := cobraCmd.Flags().GetString("domain")
			node, _ := cobraCmd.Flags().GetString("node")

			if domain == "" {
				return fmt.Errorf("--domain is required")
			}
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}

			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()

			out, err := deploy.GetCertInfo(conn, domain)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}

	cmd.Flags().String("domain", "", "Domain to check")
	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraLogsCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Manage log shipping via Promtail",
	}

	cmd.AddCommand(newInfraLogsInstallCmd(f))
	cmd.AddCommand(newInfraLogsRemoveCmd(f))

	return cmd
}

func newInfraLogsInstallCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Promtail and ship logs to Loki",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			lokiURL, _ := cobraCmd.Flags().GetString("loki")
			nodeName, _ := cobraCmd.Flags().GetString("name")
			port, _ := cobraCmd.Flags().GetInt("port")
			node, _ := cobraCmd.Flags().GetString("node")

			if lokiURL == "" {
				return fmt.Errorf("--loki URL is required")
			}
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}

			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()

			return deploy.InstallPromtail(conn, deploy.PromtailConfig{
				LokiURL:  lokiURL,
				NodeName: nodeName,
				Port:     port,
			})
		},
	}

	cmd.Flags().String("loki", "", "Loki URL (e.g. http://loki:3100)")
	cmd.Flags().StringP("name", "N", "", "Node name label")
	cmd.Flags().Int("port", 9080, "Promtail HTTP port")
	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraLogsRemoveCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove Promtail from a node",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			node, _ := cobraCmd.Flags().GetString("node")
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}
			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			return deploy.UninstallPromtail(conn)
		},
	}

	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraAlertsCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Manage Alertmanager alerting",
	}

	cmd.AddCommand(newInfraAlertsInstallCmd(f))
	cmd.AddCommand(newInfraAlertsRemoveCmd(f))
	cmd.AddCommand(newInfraAlertsRuleCmd(f))

	return cmd
}

func newInfraAlertsInstallCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Alertmanager",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			slack, _ := cobraCmd.Flags().GetString("slack")
			email, _ := cobraCmd.Flags().GetString("email")
			botToken, _ := cobraCmd.Flags().GetString("bot-token")
			chatID, _ := cobraCmd.Flags().GetString("chat-id")
			node, _ := cobraCmd.Flags().GetString("node")

			if slack == "" && email == "" && (botToken == "" || chatID == "") {
				return fmt.Errorf("need --slack, --email, or --bot-token+--chat-id")
			}
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}

			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()

			return deploy.InstallAlertmanager(conn, deploy.AlertmanagerConfig{
				SlackWebhookURL: slack,
				Email:           email,
				TelegramToken:   botToken,
				TelegramChatID:  chatID,
			})
		},
	}

	cmd.Flags().String("slack", "", "Slack webhook URL")
	cmd.Flags().String("email", "", "Email for alerts")
	cmd.Flags().String("bot-token", "", "Telegram bot token")
	cmd.Flags().String("chat-id", "", "Telegram chat ID")
	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraAlertsRemoveCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove Alertmanager from a node",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			node, _ := cobraCmd.Flags().GetString("node")
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}
			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			return deploy.UninstallAlertmanager(conn)
		},
	}

	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraAlertsRuleCmd(f *infraFlags) *cobra.Command {
	ruleCmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage alert rules",
	}

	ruleCmd.AddCommand(newInfraAlertsRuleAddCmd(f))

	return ruleCmd
}

func newInfraAlertsRuleAddCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <rule-file>",
		Short: "Upload and install an alert rule file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			node, _ := cobraCmd.Flags().GetString("node")
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}
			client := infraSSHClient(node, f.user, f.port, *f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()
			return deploy.InstallAlertRule(conn, args[0])
		},
	}

	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Manage reverse proxy (caddy, traefik, nginx)",
	}

	var setCmd = &cobra.Command{
		Use:   "set --backend <type> [--node ip]",
		Short: "Set or change the reverse proxy backend",
		Long: `Install or switch the reverse proxy on a node.

Backends: caddy (default), traefik, nginx

Examples:
  sdk-ops infra proxy set --backend caddy --node 188.xxx.xxx.xxx
  sdk-ops infra proxy set --backend traefik --node 188.xxx.xxx.xxx`,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			backend, _ := cobraCmd.Flags().GetString("backend")
			nodeIP, _ := cobraCmd.Flags().GetString("node")
			user, _ := cobraCmd.Flags().GetString("user")
			key, _ := cobraCmd.Flags().GetString("key")
			port, _ := cobraCmd.Flags().GetInt("port")
			domain, _ := cobraCmd.Flags().GetString("domain")
			email, _ := cobraCmd.Flags().GetString("email")

			if backend == "" {
				return fmt.Errorf("--backend is required (caddy, traefik, nginx)")
			}
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}
			if domain == "" {
				return fmt.Errorf("--domain is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh connect: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()

			proxy := deploy.NewProxy(deploy.ProxyType(backend))
			cfg := deploy.ProxyConfig{
				Domain:     domain,
				Email:      email,
				TargetPort: 8080,
			}
			return proxy.Install(conn, cfg)
		},
	}
	setCmd.Flags().String("backend", "", "Proxy backend: caddy, traefik, nginx")
	setCmd.Flags().StringP("node", "n", "", "Node IP address")
	setCmd.Flags().StringP("user", "u", "root", "SSH user")
	setCmd.Flags().StringP("key", "k", "", "SSH private key path")
	setCmd.Flags().IntP("port", "p", 22, "SSH port")
	setCmd.Flags().String("domain", "", "Domain name for the proxy")
	setCmd.Flags().String("email", "", "Email for Let's Encrypt")

	var statusCmd = &cobra.Command{
		Use:   "status [--node ip]",
		Short: "Show current proxy status on a node",
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			nodeIP, _ := cobraCmd.Flags().GetString("node")
			user, _ := cobraCmd.Flags().GetString("user")
			key, _ := cobraCmd.Flags().GetString("key")
			port, _ := cobraCmd.Flags().GetInt("port")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh connect: %w", err)
			}
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
				}
			}()

			detected := deploy.DetectProxy(conn)
			if detected == "" {
				fmt.Printf("  No proxy detected on %s\n", nodeIP)
				return nil
			}
			fmt.Printf("  Detected proxy: %s\n", detected)
			proxy := deploy.NewProxy(detected)
			status, _ := proxy.Status(conn)
			fmt.Print(status)
			return nil
		},
	}
	statusCmd.Flags().StringP("node", "n", "", "Node IP address")
	statusCmd.Flags().StringP("user", "u", "root", "SSH user")
	statusCmd.Flags().StringP("key", "k", "", "SSH private key path")
	statusCmd.Flags().IntP("port", "p", 22, "SSH port")

	cmd.AddCommand(setCmd)
	cmd.AddCommand(statusCmd)
	return cmd
}

var proxyCmd = newProxyCmd()

func getInfraProvider(name, apiKey, location string, projectID int) (providers.Provider, error) {
	switch name {
	case "cubepath":
		return newCubePathProvider(apiKey, projectID)
	case "hetzner":
		return newHetznerProvider(apiKey)
	case "digitalocean":
		return newDigitalOceanProvider(apiKey)
	case "vultr":
		return newVultrProvider(apiKey)
	case "aws":
		return newAWSProvider()
	case "civo":
		return newCivoProvider(apiKey, location)
	default:
		return nil, fmt.Errorf("unsupported provider: %s (supported: cubepath, hetzner, digitalocean, vultr, aws, civo)", name)
	}
}

func newCubePathProvider(apiKey string, projectID int) (providers.Provider, error) {
	if apiKey == "" {
		apiKey = os.Getenv("CUBEPATH_API_KEY")
	}
	if apiKey == "" {
		creds, _ := providers.LoadCredentials()
		if creds != nil {
			apiKey = creds.CubePathAPIKey
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("CUBEPATH_API_KEY required for cubepath")
	}
	return cubepath.New(apiKey, projectID), nil
}

func newHetznerProvider(apiKey string) (providers.Provider, error) {
	if apiKey == "" {
		apiKey = providers.LoadCredentialsFromEnv().HetznerAPIToken
	}
	if apiKey == "" {
		creds, _ := providers.LoadCredentials()
		if creds != nil {
			apiKey = creds.HetznerAPIToken
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("HETZNER_API_TOKEN required for hetzner")
	}
	return hetzner.New(apiKey), nil
}

func newDigitalOceanProvider(apiKey string) (providers.Provider, error) {
	if apiKey == "" {
		apiKey = providers.LoadCredentialsFromEnv().DigitalOceanToken
	}
	if apiKey == "" {
		creds, _ := providers.LoadCredentials()
		if creds != nil {
			apiKey = creds.DigitalOceanToken
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("DIGITALOCEAN_TOKEN required for digitalocean")
	}
	return digitalocean.New(apiKey), nil
}

func newVultrProvider(apiKey string) (providers.Provider, error) {
	if apiKey == "" {
		apiKey = providers.LoadCredentialsFromEnv().VultrAPIKey
	}
	if apiKey == "" {
		creds, _ := providers.LoadCredentials()
		if creds != nil {
			apiKey = creds.VultrAPIKey
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("VULTR_API_KEY required for vultr")
	}
	return vultr.New(apiKey), nil
}

func newAWSProvider() (providers.Provider, error) {
	region := providers.LoadCredentialsFromEnv().AWSRegion
	if region == "" {
		creds, _ := providers.LoadCredentials()
		if creds != nil {
			region = creds.AWSRegion
		}
	}
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return aws.New(region, cfg), nil
}

func newCivoProvider(apiKey, region string) (providers.Provider, error) {
	if apiKey == "" {
		apiKey = providers.LoadCredentialsFromEnv().CivoAPIKey
	}
	if apiKey == "" {
		creds, _ := providers.LoadCredentials()
		if creds != nil {
			apiKey = creds.CivoAPIKey
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("CIVO_API_KEY required for civo")
	}
	if region == "" {
		region = "LON1"
	}
	return civo.New(apiKey, region)
}

func infraSSHClient(ip, user string, port int, f infraFlags) *ssh.Client {
	return newSSHClient(ip, user, port, f.key)
}

func runInfraInit(ip string, f infraFlags) error {
	if f.provider != "" {
		p, err := getInfraProvider(f.provider, f.apiKey, f.location, f.projectID)
		if err != nil {
			return err
		}

		createCfg := providers.VPSCreateConfig{
			Label:      f.hostname,
			Plan:       f.plan,
			Location:   f.location,
			Template:   f.template,
			Hostname:   f.hostname,
			EnableIPv4: true,
			EnableIPv6: true,
		}
		if f.sshKeyIDs != "" {
			for s := range strings.SplitSeq(f.sshKeyIDs, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					createCfg.SSHKeyIDs = append(createCfg.SSHKeyIDs, s)
				}
			}
		}

		fmt.Printf("\n🔧 Creating VPS via %s...\n", f.provider)
		vps, err := p.CreateVPS(context.Background(), createCfg)
		if err != nil {
			return fmt.Errorf("create vps: %w", err)
		}
		fmt.Printf("✅ VPS created: [%s] %s @ %s\n", vps.ID, vps.Name, vps.IP)
		ip = vps.IP
	}

	fmt.Printf("\n🔧 sdk-ops infra init %s\n", ip)
	fmt.Printf("   Mode: %s\n", f.mode)
	fmt.Printf("   User: %s\n", f.user)
	fmt.Println()

	if err := runInfraInitSSH(ip, f); err != nil {
		return err
	}
	return applyInitFleetPhases(ip, f)
}

// installAllowlistOn installs the provider allowlist on one host from its
// resolved (group-inherited) configuration, then verifies with a fresh SSH
// connection. Used by init --provision-yaml so a fresh node gets the
// firewall sets AND the admin IPs from the fleet YAML.
func installAllowlistOn(pf *ProvisionFile, h ProvisionHost) error {
	r := resolveHostConfig(pf, h)
	profile, sourceRaw, err := parseAllowlistFlag(r.firewallAllowlist)
	if err != nil {
		return fmt.Errorf("allowlist %s: %w", h.Name, err)
	}
	src, err := hardening.ParseSource(sourceRaw)
	if err != nil {
		return fmt.Errorf("allowlist %s: %w", h.Name, err)
	}
	admin4, admin6, err := parseAdminIPs(r.adminIPs)
	if err != nil {
		return fmt.Errorf("allowlist %s: %w", h.Name, err)
	}
	port := h.Port
	if port == 0 {
		port = 22
	}
	f := infraFlags{user: "sdkops", key: h.SSHKey, port: port, mode: pf.Mode}
	conn, err := infraConnect(h.Host, &f)
	if err != nil {
		return fmt.Errorf("allowlist %s: %w", h.Name, err)
	}
	defer closeConn(conn)
	cfg := hardening.AllowlistConfig{
		Profile:  profile,
		SSHPorts: hardening.CurrentSSHPorts(conn, port),
		Source:   src,
		Admin4:   admin4,
		Admin6:   admin6,
		OpenWeb:  r.httpsMode == "all",
	}
	return installAndVerifyAllowlist(conn, h.Host, &f, cfg)
}

// applyInitFleetPhases finishes a fresh init with the per-host fleet phases
// from the provision YAML: security watch, firewall state watchdog, VLAN
// interface and Traefik domains — so init leaves the server fully managed
// by the internal cron jobs, with no agent and no daemons.
func applyInitFleetPhases(ip string, f infraFlags) error {
	if f.provisionYAML == "" {
		return nil
	}
	data, err := os.ReadFile(f.provisionYAML)
	if err != nil {
		return fmt.Errorf("init --provision-yaml: %w", err)
	}
	var pf ProvisionFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("init --provision-yaml: %w", err)
	}
	if _, err := validateProvision(&pf); err != nil {
		return fmt.Errorf("init --provision-yaml: %w", err)
	}
	for _, h := range pf.Hosts {
		if h.Host != ip {
			continue
		}
		fmt.Printf("\n━━━ Finishing init with fleet phases for %s ━━━\n", h.Name)
		r := resolveHostConfig(&pf, h)
		if r.firewallAllowlist != "" {
			if err := installAllowlistOn(&pf, h); err != nil {
				return fmt.Errorf("init --provision-yaml: %w", err)
			}
		}
		if err := applyPerHostPhaseOn(pf, h); err != nil {
			return fmt.Errorf("init --provision-yaml: %w", err)
		}
		fmt.Println("✅ Fleet phases applied")
		return nil
	}
	return fmt.Errorf("init --provision-yaml: host %s not found in the fleet YAML", ip)
}

func applyInfraHardening(conn *golang_ssh.Client, f infraFlags) hardening.Config {
	hardCfg := hardening.DefaultConfig()
	if f.user != "root" {
		hardCfg.User = f.user
	}
	hardCfg.EnableMonitor = f.monitor
	hardCfg.EnableAuditd = f.auditd
	hardCfg.EnableLynis = f.lynis
	hardCfg.EnableUSG = f.usg
	hardCfg.LockRoot = f.lockRoot
	if f.hardSSHPort > 0 {
		hardCfg.SSHPort = f.hardSSHPort
	}
	if err := hardening.Apply(conn, hardCfg); err != nil {
		fmt.Printf("  ⚠️  Hardening partially failed, continuing...\n")
	}
	return hardCfg
}

func reconnectAfterHardening(ip string, f infraFlags, hardCfg hardening.Config) (*golang_ssh.Client, error) {
	reconnectPort := f.port
	reconnectUser := hardCfg.User
	if hardCfg.MigrateSSH() {
		reconnectPort = hardCfg.SSHPort
	}
	fmt.Printf("  → Reconnecting as %s@%s port %d...\n", reconnectUser, ip, reconnectPort)
	for attempt := 1; attempt <= 10; attempt++ {
		reClient := infraSSHClient(ip, reconnectUser, reconnectPort, f)
		conn2, err := reClient.Connect()
		if err == nil {
			return conn2, nil
		}
		if attempt == 10 {
			keyDisplay := f.key
			if keyDisplay == "" {
				homeDir, _ := os.UserHomeDir()
				keyDisplay = filepath.Join(homeDir, ".ssh", "id_ed25519")
			}
			return nil, fmt.Errorf("reconnect: %w\n(try: ssh %s@%s -p %d -i %s)", err, reconnectUser, ip, reconnectPort, keyDisplay)
		}
		fmt.Printf("  Waiting for SSH on port %d... (attempt %d/%d)\n", reconnectPort, attempt, 10)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("reconnect: exceeded retries")
}

func askAndInstallCrowdsec(conn *golang_ssh.Client, f infraFlags) error {
	if !f.crowdsec && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("  ? Install CrowdSec (WAF/IPS)? [Y/n]: ")
		var resp string
		if _, err := fmt.Scanln(&resp); err != nil {
			log.Printf("infra: scan error: %v", err)
		}
		if resp == "" || resp == "y" || resp == "Y" || resp == "yes" {
			f.crowdsec = true
		}
	}
	if f.crowdsec {
		return installCrowdSec(conn)
	}
	return nil
}

func runInfraInitSSH(ip string, f infraFlags) error {
	client := infraSSHClient(ip, f.user, f.port, f)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
		}
	}()

	hardCfg := applyInfraHardening(conn, f)
	if err := conn.Close(); err != nil {
		log.Printf("infra: conn close error: %v", err)
	}

	conn, err = reconnectAfterHardening(ip, f, hardCfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
		}
	}()

	if err := askAndInstallCrowdsec(conn, f); err != nil {
		return err
	}

	switch f.mode {
	case "k3s":
		return runInfraInitK3s(conn, ip, f, hardCfg)
	case "docker":
		return runInfraInitDocker(conn, ip, f, hardCfg)
	default:
		return runInfraInitBare(conn, ip, f, hardCfg)
	}
}

func runInfraInitK3s(conn *golang_ssh.Client, ip string, f infraFlags, hardCfg hardening.Config) error {
	if err := docker.Install(conn); err != nil {
		return err
	}

	// Airgap: pre-download k3s binary and copy via SSH
	if f.airgap {
		if err := runInfraInitAirgap(conn, ip); err != nil {
			return err
		}
	}

	installCfg := k3s.DefaultInstallConfig(ip)
	installCfg.LocalPath = f.kubeconfig
	installCfg.Context = f.contextName
	installCfg.Merge = f.mergeConfig
	installCfg.DisableTraefik = f.disableTraefik
	installCfg.SecretsEncryption = f.secretsEncryption
	installCfg.ProtectKernelDefaults = f.protectKernelDefaults
	installCfg.AdmissionPlugins = f.admissionPlugins
	installCfg.CISPSA = f.cisPSA
	installCfg.CISAuditLog = f.cisAuditLog
	installCfg.CISNetPol = f.cisNetPol
	installCfg.CISSvcAcc = f.cisSvcAcc
	installCfg.CISTLSCiphers = f.cisTLSCiphers
	installCfg.SkipDownload = f.airgap

	if err := k3s.Install(conn, installCfg); err != nil {
		return err
	}

	return runInfraInitPostInstall(conn, ip, f, hardCfg)
}

func runInfraInitDocker(conn *golang_ssh.Client, ip string, f infraFlags, hardCfg hardening.Config) error {
	if err := docker.Install(conn); err != nil {
		return err
	}
	return runInfraInitPostInstall(conn, ip, f, hardCfg)
}

func runInfraInitBare(conn *golang_ssh.Client, ip string, f infraFlags, hardCfg hardening.Config) error {
	return runInfraInitPostInstall(conn, ip, f, hardCfg)
}

func runInfraInitPostInstall(conn *golang_ssh.Client, ip string, f infraFlags, hardCfg hardening.Config) error {
	// After hardening, SSH as root is disabled — follow-up connections
	// (allowlist verify, etc.) must use the hardened user and SSH port.
	f.user = hardCfg.User
	if hardCfg.SSHPort > 0 {
		f.port = hardCfg.SSHPort
	}
	// Phase: Log shipping (Promtail)
	if f.logsURL != "" {
		if err := deploy.InstallPromtail(conn, deploy.PromtailConfig{
			LokiURL:  f.logsURL,
			NodeName: ip,
		}); err != nil {
			return fmt.Errorf("promtail: %w", err)
		}
	}

	// Phase: Alerting (Alertmanager)
	if f.alertsURL != "" {
		if err := deploy.InstallAlertmanager(conn, deploy.AlertmanagerConfig{
			SlackWebhookURL: f.alertsURL,
		}); err != nil {
			return fmt.Errorf("alertmanager: %w", err)
		}
	}

	// Create /opt/sdk-ops/ structure
	fmt.Println("  → Creating /opt/sdk-ops/ structure...")
	createSDKOpsStructure(conn)

	// Detect architecture
	archOut, _, _ := ssh.Run(conn, "uname -m")
	arch := strings.TrimSpace(archOut)

	// Auto-register node in ~/.sdk-ops/config.yaml
	registerInitNode(ip, f, hardCfg, arch)

	// Run post-init hooks
	if err := hooks.Run(conn, "post-init", map[string]string{
		"IP":   ip,
		"USER": hardCfg.User,
		"MODE": f.mode,
		"PORT": fmt.Sprintf("%d", hardCfg.SSHPort),
	}); err != nil {
		log.Printf("infra: hooks error: %v", err)
	}

	// Phase: Traefik default reverse proxy (catch-all 404). k3s ships its own.
	if f.mode != "k3s" && !f.noTraefik {
		if err := installTraefikPhase(conn); err != nil {
			return err
		}
	}

	// Phase: provider IP allowlist (hardening goes hand in hand)
	if f.firewallAllowlist != "" {
		if err := installAllowlistPhase(conn, ip, f); err != nil {
			return err
		}
	}

	fmt.Println("\n✅ infra init complete!")
	sshHint := fmt.Sprintf("   SSH: ssh %s@%s", hardCfg.User, ip)
	if hardCfg.SSHPort > 0 {
		sshHint += fmt.Sprintf(" -p %d", hardCfg.SSHPort)
	}
	fmt.Println(sshHint)
	if f.mode == "k3s" {
		fmt.Printf("   Kubeconfig: %s\n", f.kubeconfig)
	}
	return nil
}

func downloadK3sBinary(localFile, version, arch string) error {
	suffix := "linux-amd64"
	if strings.Contains(arch, "aarch64") || strings.Contains(arch, "arm64") {
		suffix = "linux-arm64"
	}

	dlURL := fmt.Sprintf("https://github.com/k3s-io/k3s/releases/%s/download/k3s-%s", version, suffix)
	if version == "latest" {
		dlURL = fmt.Sprintf("https://github.com/k3s-io/k3s/releases/latest/download/k3s-%s", suffix)
	}

	fmt.Printf("  → Downloading %s...\n", dlURL)
	dlCmd := exec.CommandContext(context.Background(), "curl")
	dlCmd.Args = append(dlCmd.Args, "-sfLo", localFile, dlURL)
	if out, err := dlCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("download k3s binary: %w\n%s", err, string(out))
	}
	if err := os.Chmod(localFile, 0600); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}
	return nil
}

func uploadBinaryToRemote(conn *golang_ssh.Client, localFile string) error {
	fmt.Println("  → Copying binary to remote server...")
	data, err := os.ReadFile(filepath.Clean(localFile))
	if err != nil {
		return fmt.Errorf("read k3s binary: %w", err)
	}

	uploadCmd := "sudo sh -c 'cat > /usr/local/bin/k3s' && sudo chmod +x /usr/local/bin/k3s"
	sess, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		if err := sess.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: sess close error: %v\n", err)
		}
		return fmt.Errorf("stdin pipe: %w", err)
	}
	go func() {
		defer func() {
			if err := stdin.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "infra: stdin close error: %v\n", err)
			}
		}()
		if _, err := stdin.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "infra: stdin write error: %v\n", err)
		}
	}()
	if out, err := sess.CombinedOutput(uploadCmd); err != nil {
		if err := sess.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: sess close error: %v\n", err)
		}
		return fmt.Errorf("upload binary: %w\n%s", err, string(out))
	}
	if err := sess.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "infra: sess close error: %v\n", err)
	}
	fmt.Println("  ✓ Binary copied to remote server")
	return nil
}

func runInfraInitAirgap(conn *golang_ssh.Client, ip string) error {
	fmt.Println("  → Airgap mode: downloading k3s binary locally...")
	archOut, _, _ := ssh.Run(conn, "uname -m")
	arch := strings.TrimSpace(archOut)

	version := ""
	k3sVerOut, _, _ := ssh.Run(conn, "k3s --version 2>/dev/null || true")
	if strings.Contains(k3sVerOut, "k3s") {
		fmt.Println("  → k3s already installed, skipping airgap download")
	}

	localFile := "/tmp/k3s-" + ip
	if version == "" {
		version = "latest"
	}

	if err := downloadK3sBinary(localFile, version, arch); err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(localFile); err != nil {
			fmt.Fprintf(os.Stderr, "infra: remove error: %v\n", err)
		}
	}()

	return uploadBinaryToRemote(conn, localFile)
}

func runInfraJoin(serverIP, agentIP, serverUser, token string, f infraFlags) error {
	fmt.Printf("\n🔗 sdk-ops infra join %s → %s\n", agentIP, serverIP)

	if serverUser == "" {
		serverUser = f.user
	}

	// Connect to agent
	agentClient := infraSSHClient(agentIP, f.user, f.port, f)
	agentConn, err := agentClient.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect to agent: %w", err)
	}
	defer func() {
		if err := agentConn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: agent conn close error: %v\n", err)
		}
	}()

	// Connect to server (for token)
	serverClient := infraSSHClient(serverIP, serverUser, f.port, f)
	serverConn, err := serverClient.Connect()
	if err != nil {
		if token == "" {
			return fmt.Errorf("need --token (cannot SSH to server): %w", err)
		}
		fmt.Printf("  Note: cannot SSH to server, using provided token\n")
		serverConn = nil
	}
	if serverConn != nil {
		defer func() {
			if err := serverConn.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "infra: server conn close error: %v\n", err)
			}
		}()
	}

	joinCfg := k3s.JoinConfig{
		ServerIP: serverIP,
		Token:    token,
	}
	if err := k3s.Join(agentConn, serverConn, joinCfg); err != nil {
		return err
	}

	// Detect architecture on agent
	archOut, _, _ := ssh.Run(agentConn, "uname -m")
	arch := strings.TrimSpace(archOut)

	// Register agent node
	cfg, _ := loadConfig()
	found := false
	for i, n := range cfg.Nodes {
		if n.IP == agentIP {
			cfg.Nodes[i].Role = "agent"
			cfg.Nodes[i].Arch = arch
			found = true
			break
		}
	}
	if !found {
		cfg.Nodes = append(cfg.Nodes, NodeConfig{
			IP:   agentIP,
			User: f.user,
			Key:  f.key,
			Port: f.port,
			Mode: f.mode,
			Role: "agent",
			Arch: arch,
		})
	}
	if err := saveConfig(cfg); err != nil {
		log.Printf("infra: save config error: %v", err)
	}

	// Run post-join hooks on agent
	if err := hooks.Run(agentConn, "post-join", map[string]string{
		"IP":        agentIP,
		"SERVER_IP": serverIP,
		"USER":      f.user,
		"MODE":      f.mode,
		"ROLE":      "agent",
	}); err != nil {
		log.Printf("infra: hooks error: %v", err)
	}

	fmt.Printf("\n✅ Node %s joined to %s\n", agentIP, serverIP)
	fmt.Printf("   Run: export KUBECONFIG=%s\n", f.kubeconfig)
	return nil
}

func installCrowdSec(conn *golang_ssh.Client) error {
	fmt.Println("  → Installing CrowdSec...")
	script := `#!/bin/bash
set -euo pipefail
if command -v cscli &>/dev/null; then
    echo "CrowdSec already installed"
    exit 0
fi
curl -fsSL https://install.crowdsec.net | sudo sh
sudo systemctl enable crowdsec
sudo systemctl start crowdsec
echo "CrowdSec installed"
`
	out, _, err := ssh.Run(conn, script)
	if err != nil {
		return fmt.Errorf("crowdsec install failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)
	return nil
}

func runInfraStatus(ip string, f infraFlags) error {
	client := infraSSHClient(ip, f.user, f.port, f)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
		}
	}()

	fmt.Printf("\n📊 sdk-ops infra status %s\n", ip)
	fmt.Println(strings.Repeat("─", 50))

	sysInfo := `echo "Hostname: $(hostname)"
echo "Kernel:   $(uname -r)"
echo "Uptime:   $(uptime -p)"
echo "CPU:      $(nproc) cores, load: $(uptime | awk -F'load average:' '{print $2}')"
echo "Memory:   $(free -h | awk '/^Mem:/ {print $3 "/" $2}')"
echo "Disk:     $(df -h / | awk 'NR==2 {print $3 "/" $2}')"`
	out, _, err := ssh.Run(conn, sysInfo)
	if err != nil {
		return fmt.Errorf("system info: %w", err)
	}
	fmt.Print(out)
	fmt.Println(strings.Repeat("─", 50))

	// Hardening
	hardenOut, err := hardening.Check(conn)
	if err != nil {
		fmt.Printf("  hardening: %v\n", err)
	} else {
		fmt.Print(hardenOut)
	}

	// Docker
	dockerOut, err := docker.Check(conn)
	if err != nil {
		fmt.Printf("  docker: %v\n", err)
	} else {
		fmt.Print(dockerOut)
	}

	// k3s
	k3sOut, err := k3s.Check(conn)
	if err != nil {
		fmt.Printf("  k3s: %v\n", err)
	} else {
		fmt.Print(k3sOut)
	}

	fmt.Println(strings.Repeat("─", 50))
	return nil
}

func runInfraReady(ip string, f infraFlags) error {
	client := infraSSHClient(ip, f.user, f.port, f)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
		}
	}()

	fmt.Printf("\n🔍 Checking node %s...\n", ip)

	// Run k3s diagnostics
	k3sOut, err := k3s.Check(conn)
	if err != nil {
		fmt.Print(k3sOut)
		return fmt.Errorf("k3s check failed: %w", err)
	}
	fmt.Print(k3sOut)

	// Check all nodes are Ready
	nodesOut, _, _ := ssh.Run(conn, `sudo kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes --no-headers 2>/dev/null | awk '{print $2}'`)
	nodesOut = strings.TrimSpace(nodesOut)
	if nodesOut == "" {
		fmt.Println("  ✗ No nodes found (k3s may still be starting)")
		return fmt.Errorf("no nodes found")
	}

	allReady := true
	for status := range strings.SplitSeq(nodesOut, "\n") {
		if status != "Ready" {
			fmt.Printf("  ✗ Node not Ready: %s\n", status)
			allReady = false
		}
	}
	if allReady {
		fmt.Println("  ✓ All nodes Ready")
	}

	// Check core system pods
	podsOut, _, _ := ssh.Run(conn, `sudo kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get pods -n kube-system --no-headers 2>/dev/null | awk '{print $1, $3}'`)
	podsOut = strings.TrimSpace(podsOut)
	if podsOut != "" {
		allRunning := true
		for line := range strings.SplitSeq(podsOut, "\n") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] != "Running" {
				fmt.Printf("  ⚠ Pod %s is %s\n", parts[0], parts[1])
				allRunning = false
			}
		}
		if allRunning {
			fmt.Println("  ✓ All system pods Running")
		}
	}

	if !allReady {
		return fmt.Errorf("cluster not ready: some nodes are not Ready")
	}

	fmt.Println("\n✅ Cluster is ready!")
	return nil
}

type nodeAdoptInfo struct {
	hostname       string
	osInfo         string
	arch           string
	hasDocker      bool
	hasK3s         bool
	containerCount int
	services       []string
}

func collectNodeInfo(conn *golang_ssh.Client) nodeAdoptInfo {
	hostname, _, _ := ssh.Run(conn, "hostname 2>/dev/null | tr -d '\n'")
	hostname = strings.TrimSpace(hostname)
	fmt.Printf("  Hostname: %s\n", hostname)

	osInfo, _, _ := ssh.Run(conn, `(. /etc/os-release 2>/dev/null && echo "$ID $VERSION_ID") || echo "unknown"`)
	fmt.Printf("  OS:       %s", strings.TrimSpace(osInfo))
	arch, _, _ := ssh.Run(conn, "uname -m 2>/dev/null | tr -d '\n'")
	fmt.Printf("  (%s)\n", strings.TrimSpace(arch))

	dockerVer, _, _ := ssh.Run(conn, `docker --version 2>/dev/null || echo "not-installed"`)
	dockerVer = strings.TrimSpace(dockerVer)
	hasDocker := !strings.Contains(dockerVer, "not-installed") && dockerVer != ""
	if hasDocker {
		fmt.Printf("  Docker:   %s\n", dockerVer)
	} else {
		fmt.Printf("  Docker:   %snot installed%s\n", colorYellow, colorReset)
	}

	k3sVer, _, _ := ssh.Run(conn, `k3s --version 2>/dev/null | head -1 || echo "not-installed"`)
	k3sVer = strings.TrimSpace(k3sVer)
	hasK3s := !strings.Contains(k3sVer, "not-installed") && k3sVer != ""
	if hasK3s {
		fmt.Printf("  k3s:      %s\n", k3sVer)
	} else {
		fmt.Printf("  k3s:      %snot installed%s\n", colorYellow, colorReset)
	}

	containers, _, _ := ssh.Run(conn, "docker ps --format '{{.Names}}' 2>/dev/null | head -20 || true")
	containerCount := 0
	for l := range strings.SplitSeq(strings.TrimSpace(containers), "\n") {
		if strings.TrimSpace(l) != "" {
			containerCount++
		}
	}
	fmt.Printf("  Containers: %d running\n", containerCount)

	services, _ := deploy.ListServices(conn)
	if len(services) > 0 {
		fmt.Printf("  sdk-ops services: %d\n", len(services))
		for _, svc := range services {
			fmt.Printf("    - %s\n", svc)
		}
	} else {
		fmt.Printf("  sdk-ops services: %snone%s\n", colorYellow, colorReset)
	}

	hardenOut, _ := hardening.Check(conn)
	fmt.Printf("  Hardening:\n")
	for line := range strings.SplitSeq(strings.TrimSpace(hardenOut), "\n") {
		if strings.TrimSpace(line) != "" {
			fmt.Printf("    %s\n", line)
		}
	}

	return nodeAdoptInfo{
		hostname:       hostname,
		osInfo:         strings.TrimSpace(osInfo),
		arch:           strings.TrimSpace(arch),
		hasDocker:      hasDocker,
		hasK3s:         hasK3s,
		containerCount: containerCount,
		services:       services,
	}
}

func resolveAdoptMode(info nodeAdoptInfo, adoptMode string) string {
	mode := adoptMode
	if mode == "" {
		switch {
		case info.hasK3s:
			mode = "k3s"
		case info.hasDocker:
			mode = "docker"
		default:
			mode = "bare"
		}
	}
	return mode
}

func promptAdoptConfirmation(mode string) bool {
	fmt.Printf("  Register this node as --%s? [Y/n]: ", mode)
	var resp string
	if _, err := fmt.Scanln(&resp); err != nil {
		log.Printf("infra: scan error: %v", err)
	}
	return resp != "n" && resp != "N" && resp != "no"
}

func registerAdoptedNode(ip string, f infraFlags, mode string, info nodeAdoptInfo) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	found := false
	for i, n := range cfg.Nodes {
		if n.IP == ip {
			cfg.Nodes[i].Mode = mode
			cfg.Nodes[i].Hostname = info.hostname
			cfg.Nodes[i].Arch = info.arch
			if cfg.Nodes[i].User == "" {
				cfg.Nodes[i].User = f.user
			}
			found = true
			break
		}
	}
	if !found {
		cfg.Nodes = append(cfg.Nodes, NodeConfig{
			IP:       ip,
			User:     f.user,
			Key:      f.key,
			Port:     f.port,
			Mode:     mode,
			Role:     "server",
			Arch:     info.arch,
			Hostname: info.hostname,
		})
	}
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("  %s✓ Node %s registered (mode: %s)%s\n", colorGreen, ip, mode, colorReset)
	return nil
}

func syncAdoptedState(conn *golang_ssh.Client, ip, mode string, info nodeAdoptInfo) {
	if !info.hasDocker {
		return
	}
	fmt.Println("  Syncing state...")
	for _, svc := range info.services {
		svcStatus := "ok"
		if s, _ := deploy.ServiceStatus(conn, svc); s != "" && !strings.Contains(s, "running") && !strings.Contains(s, "type:") {
			svcStatus = "unknown"
		}
		stateRecord("service", svc, ip, "adopted", mode, svcStatus, nil)
	}
}

func runInfraAdopt(ip string, f infraFlags, forced bool, adoptMode string) error {
	client := infraSSHClient(ip, f.user, f.port, f)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh %s: %w", ip, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
		}
	}()

	fmt.Printf("\n  Scanning %s...\n", ip)

	info := collectNodeInfo(conn)
	mode := resolveAdoptMode(info, adoptMode)
	fmt.Printf("\n  Detected mode: %s\n", mode)

	if !forced {
		if !promptAdoptConfirmation(mode) {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	if err := registerAdoptedNode(ip, f, mode, info); err != nil {
		return err
	}
	syncAdoptedState(conn, ip, mode, info)
	return nil
}

func runInfraRemove(ip string, f infraFlags) error {
	client := infraSSHClient(ip, f.user, f.port, f)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "infra: conn close error: %v\n", err)
		}
	}()

	fmt.Printf("\n🗑️  sdk-ops infra remove %s\n", ip)

	out, _, err := ssh.Run(conn, "command -v k3s && echo 'k3s: yes' || echo 'k3s: no'; command -v docker && echo 'docker: yes' || echo 'docker: no'")
	if err != nil {
		return fmt.Errorf("check installed: %w", err)
	}
	fmt.Print(out)

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("  → Skipping uninstall (non-interactive)")
		return nil
	}

	fmt.Print("  ? Remove all sdk-ops-installed components? [y/N]: ")
	var resp string
	if _, err := fmt.Scanln(&resp); err != nil {
		log.Printf("infra: scan error: %v", err)
	}
	if resp != "y" && resp != "Y" && resp != "yes" {
		fmt.Println("  Aborted.")
		return nil
	}

	runInfraRemoveComponents(conn, ip, f)
	return nil
}

func runInfraRemoveComponents(conn *golang_ssh.Client, ip string, f infraFlags) {
	// Run pre-remove hooks
	if err := hooks.Run(conn, "pre-remove", map[string]string{
		"IP":   ip,
		"USER": f.user,
	}); err != nil {
		log.Printf("infra: hooks error: %v", err)
	}

	scripts := []string{
		"k3s-uninstall.sh",
		"/usr/local/bin/k3s-killall.sh",
	}
	for _, s := range scripts {
		if _, _, err := ssh.Run(conn, fmt.Sprintf("test -f %s && %s || true", s, s)); err != nil {
			log.Printf("infra: ssh run error: %v", err)
		}
	}

	if _, _, err := ssh.Run(conn, `apt-get remove -y docker-ce docker-ce-cli containerd.io docker-compose-plugin 2>/dev/null || true`); err != nil {
		log.Printf("infra: ssh run error: %v", err)
	}
	if _, _, err := ssh.Run(conn, `rm -rf /opt/sdk-ops`); err != nil {
		log.Printf("infra: ssh run error: %v", err)
	}
	if _, _, err := ssh.Run(conn, `rm -f /etc/sudoers.d/sdk-ops`); err != nil {
		log.Printf("infra: ssh run error: %v", err)
	}

	// Run post-remove hooks
	if err := hooks.Run(conn, "post-remove", map[string]string{
		"IP":   ip,
		"USER": f.user,
	}); err != nil {
		log.Printf("infra: hooks error: %v", err)
	}

	fmt.Println("✅ sdk-ops removed from", ip)
}

func firewalledNode(cmd *cobra.Command) string {
	n, _ := cmd.Flags().GetString("node")
	if n == "" {
		if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
			n = cfg.Nodes[0].IP
		}
	}
	return n
}
