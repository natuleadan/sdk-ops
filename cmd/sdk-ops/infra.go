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

	"github.com/natuleadan/sdk-ops/cloudinit"
	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/docker"
	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/hooks"
	"github.com/natuleadan/sdk-ops/k3s"
	"github.com/natuleadan/sdk-ops/plan"
	"github.com/natuleadan/sdk-ops/providers"
	"github.com/natuleadan/sdk-ops/providers/aws"
	"github.com/natuleadan/sdk-ops/providers/cubepath"
	"github.com/natuleadan/sdk-ops/providers/digitalocean"
	"github.com/natuleadan/sdk-ops/providers/hetzner"
	"github.com/natuleadan/sdk-ops/providers/vultr"
	"github.com/natuleadan/sdk-ops/ssh"
)

type infraFlags struct {
	user          string
	key           string
	port          int
	mode          string // k3s, docker, bare
	crowdsec      bool
	cloudInit     bool
	cloudInitOnly bool
	airgap        bool
	monitor       bool
	auditd        bool
	lynis         bool
	usg           bool
	lockRoot      bool
	hardSSHPort   int
	logsURL       string
	alertsURL     string
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
	cmd.AddCommand(newInfraBackupCmd(&f))
	cmd.AddCommand(newInfraRestoreCmd(&f))
	cmd.AddCommand(newInfraCertCmd(&f))
	cmd.AddCommand(newInfraLogsCmd(&f))
	cmd.AddCommand(newInfraAlertsCmd(&f))
	cmd.AddCommand(planCmd())
	cmd.AddCommand(applyCmd())
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
	cmd.Flags().BoolVar(&f.cloudInit, "cloud-init", false, "Use cloud-init instead of SSH-based provisioning")
	cmd.Flags().BoolVar(&f.cloudInitOnly, "cloud-init-only", false, "Generate and print cloud-init user-data only")
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

	return cmd
}

func newInfraFirewallOpenCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <port>",
		Short: "Open a port in the firewall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			port := 0
			if _, err := fmt.Sscanf(args[0], "%d", &port); err != nil {
				log.Printf("infra: parse port error: %v", err)
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port: %s", args[0])
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
			return hardening.FirewallOpen(conn, port, proto)
		},
	}

	cmd.Flags().StringP("proto", "P", "tcp", "Protocol (tcp, udp)")
	cmd.Flags().StringP("node", "n", "", "Target node IP")

	return cmd
}

func newInfraFirewallCloseCmd(f *infraFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close <port>",
		Short: "Close a port in the firewall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			port := 0
			if _, err := fmt.Sscanf(args[0], "%d", &port); err != nil {
				log.Printf("infra: parse port error: %v", err)
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port: %s", args[0])
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
			return hardening.FirewallClose(conn, port, proto)
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

func planCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan <file.yaml>",
		Short: "Validate and preview an infrastructure plan",
		Long: `Parse a YAML plan file and show what will be provisioned.
The plan file defines servers, agents, and SSH options.

Example plan.yaml:
  mode: k3s
  parallel: 5
  server_options:
    user: root
    ssh_key: ~/.ssh/id_ed25519
    k3s_extra_args: "--disable traefik"
  agent_options:
    user: root
  hosts:
    - name: server-1
      role: server
      host: 192.168.1.10
    - name: agent-1
      role: agent
      host: 192.168.1.11`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := plan.ParseFile(args[0])
			if err != nil {
				return fmt.Errorf("invalid plan: %w", err)
			}
			fmt.Print(p.Summary())
			return nil
		},
	}
}

func applyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <plan.yaml>",
		Short: "Execute an infrastructure plan",
		Long: `Provision all hosts defined in a plan file.
Installs servers first, then joins agents — all in parallel.

Examples:
  sdk-ops infra apply plan.yaml
  sdk-ops infra apply plan.yaml --parallel 10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := plan.ParseFile(args[0])
			if err != nil {
				return fmt.Errorf("invalid plan: %w", err)
			}

			fmt.Println("📋 Plan:")
			fmt.Print(p.Summary())

			results := plan.Apply(p, gInsecure)

			errs := 0
			for _, r := range results {
				if r.Error != nil {
					fmt.Printf("  ✗ %s [%s]: %v\n", r.Host, r.Step, r.Error)
					errs++
				} else {
					fmt.Printf("  ✓ %s [%s]: OK\n", r.Host, r.Step)
				}
			}

			if errs > 0 {
				return fmt.Errorf("%d errors during apply", errs)
			}
			fmt.Println("\n✅ Plan applied successfully!")
			return nil
		},
	}
}

func getInfraProvider(name, apiKey string, projectID int) (providers.Provider, error) {
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
	default:
		return nil, fmt.Errorf("unsupported provider: %s (supported: cubepath, hetzner, digitalocean, vultr, aws)", name)
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
		apiKey = os.Getenv("HETZNER_API_TOKEN")
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
		apiKey = os.Getenv("DIGITALOCEAN_TOKEN")
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
		apiKey = os.Getenv("VULTR_API_KEY")
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
	region := os.Getenv("AWS_REGION")
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

func infraSSHClient(ip, user string, port int, f infraFlags) *ssh.Client {
	return newSSHClient(ip, user, port, f.key)
}

func sshPublicKeys() []string {
	home, _ := os.UserHomeDir()
	pubPath := filepath.Join(home, ".ssh", "id_ed25519.pub")
	data, err := os.ReadFile(filepath.Clean(pubPath))
	if err != nil {
		return nil
	}
	return []string{strings.TrimSpace(string(data))}
}

func runInfraInit(ip string, f infraFlags) error {
	if f.cloudInitOnly {
		return runInfraInitCloudInitOnly(f)
	}

	if f.provider != "" {
		p, err := getInfraProvider(f.provider, f.apiKey, f.projectID)
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
				var id int
				if _, err := fmt.Sscanf(s, "%d", &id); err != nil {
					log.Printf("infra: parse id error: %v", err)
				}
				if id > 0 {
					createCfg.SSHKeyIDs = append(createCfg.SSHKeyIDs, id)
				}
			}
		}

		if f.cloudInit {
			ciCfg := cloudinit.DefaultConfig()
			ciCfg.Mode = f.mode
			ciCfg.CrowdSec = f.crowdsec
			ciCfg.EnableMonitor = f.monitor
			ciCfg.DisableTraefik = f.disableTraefik
			userData := cloudinit.Generate(ciCfg)
			createCfg.UserData = userData
			fmt.Println("  → Cloud-init user-data generated")
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

	if f.cloudInit {
		return runInfraInitCloudInit(ip, &f)
	}

	return runInfraInitSSH(ip, f)
}

func runInfraInitCloudInitOnly(f infraFlags) error {
	ciCfg := cloudinit.DefaultConfig()
	ciCfg.SSHKeys = sshPublicKeys()
	switch f.mode {
	case "docker":
		ciCfg.Mode = "docker"
	case "bare":
		ciCfg.Mode = "bare"
	}
	if f.hardSSHPort > 0 {
		ciCfg.SSHPort = f.hardSSHPort
	}
	ciCfg.CrowdSec = f.crowdsec
	ciCfg.EnableMonitor = f.monitor
	ciCfg.DisableTraefik = f.disableTraefik
	fmt.Println(cloudinit.Generate(ciCfg))
	return nil
}

func runInfraInitCloudInit(ip string, f *infraFlags) error {
	fmt.Println("  → Cloud-init mode: waiting for VPS to boot...")
	time.Sleep(10 * time.Second)

	ciUser := f.user
	ciPort := f.port
	if ciUser == "root" {
		ciUser = "sdkops"
		ciPort = 2222
	}
	for attempt := 1; attempt <= 30; attempt++ {
		client := infraSSHClient(ip, ciUser, ciPort, *f)
		conn, err := client.Connect()
		if err == nil {
			if err := conn.Close(); err != nil {
				log.Printf("infra: conn close error: %v", err)
			}
			f.user = ciUser
			f.port = ciPort
			break
		}
		if attempt == 30 {
			return fmt.Errorf("cloud-init: VPS not ready after 150s")
		}
		time.Sleep(5 * time.Second)
	}

	client := infraSSHClient(ip, f.user, f.port, *f)
	conn, err := client.Connect()
	if err == nil {
		if _, _, sErr := ssh.Run(conn, `mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs 2>/dev/null; echo "sdk-ops-init" > /opt/sdk-ops/.version 2>/dev/null || true`); sErr != nil {
			log.Printf("infra: ssh run error: %v", sErr)
		}
		if err := conn.Close(); err != nil {
			log.Printf("infra: conn close error: %v", err)
		}
	}

	cfg, _ := loadConfig()
	found := false
	for _, n := range cfg.Nodes {
		if n.IP == ip {
			found = true
			break
		}
	}
	if !found {
		cfg.Nodes = append(cfg.Nodes, NodeConfig{
			IP:   ip,
			User: ciUser,
			Key:  f.key,
			Port: ciPort,
			Mode: f.mode,
		})
		if err := saveConfig(cfg); err != nil {
			log.Printf("infra: save config error: %v", err)
		}
	}

	fmt.Println("\n✅ infra init complete (cloud-init)!")
	fmt.Printf("   SSH: ssh %s@%s -p %d\n", ciUser, ip, ciPort)
	if f.mode == "k3s" {
		fmt.Printf("   Kubeconfig: %s (fetch from server)\n", f.kubeconfig)
	}
	return nil
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
	if _, _, err := ssh.Run(conn, `mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs && echo "sdk-ops-init" > /opt/sdk-ops/.version`); err != nil {
		log.Printf("infra: ssh run error: %v", err)
	}

	// Detect architecture
	archOut, _, _ := ssh.Run(conn, "uname -m")
	arch := strings.TrimSpace(archOut)

	// Auto-register node in ~/.sdk-ops/config.yaml
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
	} else {
		if err := saveConfig(cfg); err != nil {
			log.Printf("infra: save config error: %v", err)
		}
	}

	// Run post-init hooks
	if err := hooks.Run(conn, "post-init", map[string]string{
		"IP":   ip,
		"USER": hardCfg.User,
		"MODE": f.mode,
		"PORT": fmt.Sprintf("%d", hardCfg.SSHPort),
	}); err != nil {
		log.Printf("infra: hooks error: %v", err)
	}

	fmt.Println("\n✅ infra init complete!")
	fmt.Printf("   SSH: ssh %s@%s -p %d\n", hardCfg.User, ip, hardCfg.SSHPort)
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
