package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"
	golang_ssh "golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/natuleadan/sdk-ops/docker"
	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/k3s"
	"github.com/natuleadan/sdk-ops/providers"
	"github.com/natuleadan/sdk-ops/providers/aws"
	"github.com/natuleadan/sdk-ops/providers/cubepath"
	"github.com/natuleadan/sdk-ops/providers/digitalocean"
	"github.com/natuleadan/sdk-ops/providers/hetzner"
	"github.com/natuleadan/sdk-ops/providers/vultr"
	"github.com/natuleadan/sdk-ops/cloudinit"
	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/ssh"
)

type infraFlags struct {
	user        string
	key         string
	port        int
	insecure    bool
	mode        string // k3s, docker, bare
	crowdsec    bool
	cloudInit   bool
	monitor     bool
	lockRoot    bool
	hardSSHPort int
	logsURL     string
	alertsURL   string
	// k3s-specific
	disableTraefik bool
	kubeconfig     string
	mergeConfig    bool
	contextName    string
	// provider-specific
	provider   string
	plan       string
	location   string
	template   string
	hostname   string
	sshKeyIDs  string
	apiKey     string
	projectID  int
}

func newInfraCmd() *cobra.Command {
	var f infraFlags

	var cmd = &cobra.Command{
		Use:   "infra",
		Short: "Provision and manage VPS infrastructure",
	}

	var initCmd = &cobra.Command{
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
  --plan          VPS plan (e.g. gp.nano)
  --location      Location (e.g. us-mia-1)
  --template      OS template (e.g. ubuntu-24)
  --ssh-key-ids   Comma-separated SSH key IDs
  --api-key       API key for the provider
  --project-id    Project ID for the provider (default: 4601)

Examples:
  sdk-ops infra init 188.xxx.xxx.xxx
  sdk-ops infra init --provider cubepath --plan gp.nano --location us-mia-1
  sdk-ops infra init --provider vultr --plan vc2-1c-2gb --location ewr
  sdk-ops infra init 188.xxx.xxx.xxx --docker --crowdsec`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ip := ""
			if len(args) > 0 {
				ip = args[0]
			}
			return runInfraInit(ip, f)
		},
	}

	var joinCmd = &cobra.Command{
		Use:   "join <server-ip> <agent-ip>",
		Short: "Join a worker node to a k3s cluster",
		Long: `Join a worker/agent node to an existing k3s cluster.

  --server-user   SSH user for the server (default: same as --user)
  --token         Cluster token (auto-fetched if SSH access to server)

Examples:
  sdk-ops infra join 188.xxx.xxx.100 188.xxx.xxx.101
  sdk-ops infra join 188.xxx.xxx.100 188.xxx.xxx.101 --token mytoken`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverUser, _ := cmd.Flags().GetString("server-user")
			token, _ := cmd.Flags().GetString("token")
			return runInfraJoin(args[0], args[1], serverUser, token, f)
		},
	}

	var statusCmd = &cobra.Command{
		Use:   "status <ip>",
		Short: "Show server health and installed components",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfraStatus(args[0], f)
		},
	}

	var removeCmd = &cobra.Command{
		Use:   "remove <ip>",
		Short: "Remove sdk-ops from a server (uninstall k3s/Docker)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfraRemove(args[0], f)
		},
	}

	// Global persistent flags
	cmd.PersistentFlags().StringVarP(&f.user, "user", "u", "root", "SSH user")
	cmd.PersistentFlags().StringVarP(&f.key, "key", "k", "", "SSH private key path (default: ~/.ssh/id_ed25519)")
	cmd.PersistentFlags().IntVarP(&f.port, "port", "p", 22, "SSH port")
	// --insecure is on root command only (gInsecure)

	// Init flags
	initCmd.Flags().StringVar(&f.mode, "mode", "k3s", "Installation mode: k3s, docker, bare")
	initCmd.Flags().BoolVar(&f.crowdsec, "crowdsec", false, "Install CrowdSec (WAF/IPS)")
	initCmd.Flags().BoolVar(&f.monitor, "monitor", false, "Install Prometheus node_exporter (port 9100)")
	initCmd.Flags().BoolVar(&f.lockRoot, "lock-root", false, "Lock root password after creating sdkops user")
	initCmd.Flags().IntVar(&f.hardSSHPort, "ssh-port", 0, "Migrate SSH to custom port (0=keep port 22)")
	initCmd.Flags().StringVar(&f.logsURL, "logs", "", "Install Promtail and ship logs to this Loki URL")
	initCmd.Flags().StringVar(&f.alertsURL, "alerts", "", "Install Alertmanager with this Slack webhook URL")
	initCmd.Flags().BoolVar(&f.disableTraefik, "disable-traefik", false, "Disable Traefik ingress in k3s")
	initCmd.Flags().StringVar(&f.kubeconfig, "kubeconfig", "./kubeconfig", "Path to save kubeconfig")
	initCmd.Flags().BoolVar(&f.mergeConfig, "merge", false, "Merge kubeconfig into ~/.kube/config")
	initCmd.Flags().StringVar(&f.contextName, "context", "sdk-ops-cluster", "Kubeconfig context name")

	initCmd.Flags().Bool("k3s", false, "Install Docker + k3s")
	initCmd.Flags().Bool("docker", false, "Install Docker only")
	initCmd.Flags().Bool("bare", false, "Only harden the OS")

	// Provider flags
	initCmd.Flags().StringVar(&f.provider, "provider", "", "Create VPS via provider (cubepath, hetzner, digitalocean, vultr, aws)")
	initCmd.Flags().StringVar(&f.plan, "plan", "gp.nano", "VPS plan")
	initCmd.Flags().StringVar(&f.location, "location", "us-mia-1", "VPS location")
	initCmd.Flags().StringVar(&f.template, "template", "ubuntu-24", "OS template")
	initCmd.Flags().StringVar(&f.hostname, "hostname", "", "VPS hostname")
	initCmd.Flags().StringVar(&f.sshKeyIDs, "ssh-key-ids", "", "SSH key IDs (comma-separated)")
	initCmd.Flags().StringVar(&f.apiKey, "api-key", "", "Provider API key (or provider-specific env var)")
	initCmd.Flags().IntVar(&f.projectID, "project-id", 4601, "Provider project ID")
	initCmd.Flags().BoolVar(&f.cloudInit, "cloud-init", false, "Use cloud-init instead of SSH-based provisioning")

	initCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		useK3s, _ := cmd.Flags().GetBool("k3s")
		useDocker, _ := cmd.Flags().GetBool("docker")
		useBare, _ := cmd.Flags().GetBool("bare")
		if useK3s {
			f.mode = "k3s"
		} else if useDocker {
			f.mode = "docker"
		} else if useBare {
			f.mode = "bare"
		}
		return nil
	}

	// Join flags
	joinCmd.Flags().String("server-user", "", "SSH user for the server (default: same as --user)")
	joinCmd.Flags().String("token", "", "Cluster token (auto-fetched if SSH access to server)")

	firewallCmd := &cobra.Command{
		Use:   "firewall",
		Short: "Manage firewall rules on a node",
	}

	fwNode := func(cmd *cobra.Command) string {
		n, _ := cmd.Flags().GetString("node")
		if n == "" {
			if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
				n = cfg.Nodes[0].IP
			}
		}
		return n
	}

	firewallOpenCmd := &cobra.Command{
		Use:   "open <port>",
		Short: "Open a port in the firewall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port := 0
			fmt.Sscanf(args[0], "%d", &port)
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port: %s", args[0])
			}
			proto, _ := cmd.Flags().GetString("proto")
			node := fwNode(cmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			return hardening.FirewallOpen(conn, port, proto)
		},
	}
	firewallOpenCmd.Flags().StringP("proto", "P", "tcp", "Protocol (tcp, udp)")
	firewallOpenCmd.Flags().StringP("node", "n", "", "Target node IP")

	firewallCloseCmd := &cobra.Command{
		Use:   "close <port>",
		Short: "Close a port in the firewall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			port := 0
			fmt.Sscanf(args[0], "%d", &port)
			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port: %s", args[0])
			}
			proto, _ := cmd.Flags().GetString("proto")
			node := fwNode(cmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			return hardening.FirewallClose(conn, port, proto)
		},
	}
	firewallCloseCmd.Flags().StringP("proto", "P", "tcp", "Protocol (tcp, udp)")
	firewallCloseCmd.Flags().StringP("node", "n", "", "Target node IP")

	firewallListCmd := &cobra.Command{
		Use:   "list",
		Short: "List firewall rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			node := fwNode(cmd)
			if node == "" {
				return fmt.Errorf("no node specified. Use --node or register one")
			}
			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			out, err := hardening.FirewallList(conn)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
	firewallListCmd.Flags().StringP("node", "n", "", "Target node IP")

	firewallCmd.AddCommand(firewallOpenCmd)
	firewallCmd.AddCommand(firewallCloseCmd)
	firewallCmd.AddCommand(firewallListCmd)

	backupCmd := &cobra.Command{
		Use:   "backup <ip>",
		Short: "Backup all services from a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := infraSSHClient(args[0], f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			path, err := deploy.BackupServices(conn, ".")
			if err != nil {
				return err
			}
			fmt.Printf("✅ Backup: %s\n", path)
			return nil
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <ip> <backup-file>",
		Short: "Restore services from a backup file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := infraSSHClient(args[0], f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			if err := deploy.RestoreServices(conn, args[1]); err != nil {
				return err
			}
			fmt.Println("✅ Restore complete")
			return nil
		},
	}

	certCmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage TLS certificates via Caddy",
	}

	certInstallCmd := &cobra.Command{
		Use:   "install",
		Short: "Install Caddy and provision TLS cert for a domain",
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, _ := cmd.Flags().GetString("domain")
			email, _ := cmd.Flags().GetString("email")
			port, _ := cmd.Flags().GetInt("port")
			staging, _ := cmd.Flags().GetBool("staging")
			node, _ := cmd.Flags().GetString("node")

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

			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			return deploy.InstallCaddy(conn, deploy.CaddyConfig{
				Domain:     domain,
				Email:      email,
				TargetPort: port,
				Staging:    staging,
			})
		},
	}
	certInstallCmd.Flags().String("domain", "", "Domain to provision TLS for")
	certInstallCmd.Flags().String("email", "", "Email for Let's Encrypt")
	certInstallCmd.Flags().Int("port", 8080, "Local port to proxy")
	certInstallCmd.Flags().Bool("staging", false, "Use Let's Encrypt staging environment")
	certInstallCmd.Flags().StringP("node", "n", "", "Target node IP")

	certInfoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show TLS cert info for a domain",
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, _ := cmd.Flags().GetString("domain")
			node, _ := cmd.Flags().GetString("node")

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

			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			out, err := deploy.GetCertInfo(conn, domain)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
	certInfoCmd.Flags().String("domain", "", "Domain to check")
	certInfoCmd.Flags().StringP("node", "n", "", "Target node IP")

	certCmd.AddCommand(certInstallCmd)
	certCmd.AddCommand(certInfoCmd)

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Manage log shipping via Promtail",
	}

	logsInstallCmd := &cobra.Command{
		Use:   "install",
		Short: "Install Promtail and ship logs to Loki",
		RunE: func(cmd *cobra.Command, args []string) error {
			lokiURL, _ := cmd.Flags().GetString("loki")
			nodeName, _ := cmd.Flags().GetString("name")
			port, _ := cmd.Flags().GetInt("port")
			node, _ := cmd.Flags().GetString("node")

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

			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			return deploy.InstallPromtail(conn, deploy.PromtailConfig{
				LokiURL:  lokiURL,
				NodeName: nodeName,
				Port:     port,
			})
		},
	}
	logsInstallCmd.Flags().String("loki", "", "Loki URL (e.g. http://loki:3100)")
	logsInstallCmd.Flags().StringP("name", "N", "", "Node name label")
	logsInstallCmd.Flags().Int("port", 9080, "Promtail HTTP port")
	logsInstallCmd.Flags().StringP("node", "n", "", "Target node IP")

	logsRemoveCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove Promtail from a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			node, _ := cmd.Flags().GetString("node")
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}
			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			return deploy.UninstallPromtail(conn)
		},
	}
	logsRemoveCmd.Flags().StringP("node", "n", "", "Target node IP")

	logsCmd.AddCommand(logsInstallCmd)
	logsCmd.AddCommand(logsRemoveCmd)

	alertsCmd := &cobra.Command{
		Use:   "alerts",
		Short: "Manage Alertmanager alerting",
	}

	alertsInstallCmd := &cobra.Command{
		Use:   "install",
		Short: "Install Alertmanager",
		RunE: func(cmd *cobra.Command, args []string) error {
			slack, _ := cmd.Flags().GetString("slack")
			email, _ := cmd.Flags().GetString("email")
			botToken, _ := cmd.Flags().GetString("bot-token")
			chatID, _ := cmd.Flags().GetString("chat-id")
			node, _ := cmd.Flags().GetString("node")

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

			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			return deploy.InstallAlertmanager(conn, deploy.AlertmanagerConfig{
				SlackWebhookURL: slack,
				Email:           email,
				TelegramToken:   botToken,
				TelegramChatID:  chatID,
			})
		},
	}
	alertsInstallCmd.Flags().String("slack", "", "Slack webhook URL")
	alertsInstallCmd.Flags().String("email", "", "Email for alerts")
	alertsInstallCmd.Flags().String("bot-token", "", "Telegram bot token")
	alertsInstallCmd.Flags().String("chat-id", "", "Telegram chat ID")
	alertsInstallCmd.Flags().StringP("node", "n", "", "Target node IP")

	alertsRemoveCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove Alertmanager from a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			node, _ := cmd.Flags().GetString("node")
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}
			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			return deploy.UninstallAlertmanager(conn)
		},
	}
	alertsRemoveCmd.Flags().StringP("node", "n", "", "Target node IP")

	alertsRuleCmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage alert rules",
	}

	alertsRuleAddCmd := &cobra.Command{
		Use:   "add <rule-file>",
		Short: "Upload and install an alert rule file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, _ := cmd.Flags().GetString("node")
			if node == "" {
				if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
					node = cfg.Nodes[0].IP
				}
			}
			if node == "" {
				return fmt.Errorf("no node specified")
			}
			client := infraSSHClient(node, f.user, f.port, f)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()
			return deploy.InstallAlertRule(conn, args[0])
		},
	}
	alertsRuleAddCmd.Flags().StringP("node", "n", "", "Target node IP")

	alertsRuleCmd.AddCommand(alertsRuleAddCmd)

	alertsCmd.AddCommand(alertsInstallCmd)
	alertsCmd.AddCommand(alertsRemoveCmd)
	alertsCmd.AddCommand(alertsRuleCmd)

	cmd.AddCommand(initCmd)
	cmd.AddCommand(joinCmd)
	cmd.AddCommand(statusCmd)
	cmd.AddCommand(removeCmd)
	cmd.AddCommand(firewallCmd)
	cmd.AddCommand(certCmd)
	cmd.AddCommand(logsCmd)
	cmd.AddCommand(alertsCmd)
	cmd.AddCommand(backupCmd)
	cmd.AddCommand(restoreCmd)

	return cmd
}

func getInfraProvider(name, apiKey string, projectID int) (providers.Provider, error) {
	creds, _ := providers.LoadCredentials()

	switch name {
	case "cubepath":
		if apiKey == "" {
			apiKey = os.Getenv("CUBEPATH_API_KEY")
		}
		if apiKey == "" && creds != nil {
			apiKey = creds.CubePathAPIKey
		}
		if apiKey == "" {
			return nil, fmt.Errorf("CUBEPATH_API_KEY required for cubepath")
		}
		return cubepath.New(apiKey, projectID), nil

	case "hetzner":
		if apiKey == "" {
			apiKey = os.Getenv("HETZNER_API_TOKEN")
		}
		if apiKey == "" && creds != nil {
			apiKey = creds.HetznerAPIToken
		}
		if apiKey == "" {
			return nil, fmt.Errorf("HETZNER_API_TOKEN required for hetzner")
		}
		return hetzner.New(apiKey), nil

	case "digitalocean":
		if apiKey == "" {
			apiKey = os.Getenv("DIGITALOCEAN_TOKEN")
		}
		if apiKey == "" && creds != nil {
			apiKey = creds.DigitalOceanToken
		}
		if apiKey == "" {
			return nil, fmt.Errorf("DIGITALOCEAN_TOKEN required for digitalocean")
		}
		return digitalocean.New(apiKey), nil

	case "vultr":
		if apiKey == "" {
			apiKey = os.Getenv("VULTR_API_KEY")
		}
		if apiKey == "" && creds != nil {
			apiKey = creds.VultrAPIKey
		}
		if apiKey == "" {
			return nil, fmt.Errorf("VULTR_API_KEY required for vultr")
		}
		return vultr.New(apiKey), nil

	case "aws":
		region := os.Getenv("AWS_REGION")
		if region == "" && creds != nil {
			region = creds.AWSRegion
		}
		if region == "" {
			region = "us-east-1"
		}
		cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("aws config: %w", err)
		}
		return aws.New(region, cfg), nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s (supported: cubepath, hetzner, digitalocean, vultr, aws)", name)
	}
}

func infraSSHClient(ip, user string, port int, f infraFlags) *ssh.Client {
	return newSSHClient(ip, user, port, f.key)
}

func runInfraInit(ip string, f infraFlags) error {
	// Phase 0: Create VPS via provider (if --provider is set)
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
			for _, s := range strings.Split(f.sshKeyIDs, ",") {
				var id int
				fmt.Sscanf(s, "%d", &id)
				if id > 0 {
					createCfg.SSHKeyIDs = append(createCfg.SSHKeyIDs, id)
				}
			}
		}

		// Cloud-init: generate user-data
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
		// Cloud-init path: wait for SSH, skip hardening/Docker/k3s (already done)
		fmt.Println("  → Cloud-init mode: waiting for VPS to boot...")
		time.Sleep(10 * time.Second)

		ciUser := f.user
		ciPort := f.port
		if ciUser == "root" {
			ciUser = "sdkops"
			ciPort = 2222
		}
		for attempt := 1; attempt <= 30; attempt++ {
			client := infraSSHClient(ip, ciUser, ciPort, f)
			conn, err := client.Connect()
			if err == nil {
				conn.Close()
				f.user = ciUser
				f.port = ciPort
				break
			}
			if attempt == 30 {
				return fmt.Errorf("cloud-init: VPS not ready after 150s")
			}
			time.Sleep(5 * time.Second)
		}

		// Create /opt/sdk-ops/ structure (if cloud-init didn't)
		client := infraSSHClient(ip, f.user, f.port, f)
		conn, err := client.Connect()
		if err == nil {
			ssh.Run(conn, `mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs 2>/dev/null; echo "sdk-ops-init" > /opt/sdk-ops/.version 2>/dev/null || true`)
			conn.Close()
		}

		// Register node
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
			saveConfig(cfg)
		}

		fmt.Println("\n✅ infra init complete (cloud-init)!")
		fmt.Printf("   SSH: ssh %s@%s -p %d\n", ciUser, ip, ciPort)
		if f.mode == "k3s" {
			fmt.Printf("   Kubeconfig: %s (fetch from server)\n", f.kubeconfig)
		}
		return nil
	}

	client := infraSSHClient(ip, f.user, f.port, f)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer conn.Close()

	// Phase 1: Hardening (step by step)
	hardCfg := hardening.DefaultConfig()
	if f.user != "root" {
		hardCfg.User = f.user
	}
	hardCfg.EnableMonitor = f.monitor
	hardCfg.LockRoot = f.lockRoot
	if f.hardSSHPort > 0 {
		hardCfg.SSHPort = f.hardSSHPort
	}
	if err := hardening.Apply(conn, hardCfg); err != nil {
		fmt.Printf("  ⚠️  Hardening partially failed, continuing...\n")
	}
	conn.Close()

	// Reconnect (port may have changed if --ssh-port was set)
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
			conn = conn2
			break
		}
		if attempt == 10 {
			keyDisplay := f.key
			if keyDisplay == "" {
				keyDisplay = os.ExpandEnv("$HOME/.ssh/id_ed25519")
			}
			return fmt.Errorf("reconnect: %w\n(try: ssh %s@%s -p %d -i %s)", err, reconnectUser, ip, reconnectPort, keyDisplay)
		}
		fmt.Printf("  Waiting for SSH on port %d... (attempt %d/%d)\n", reconnectPort, attempt, 10)
		time.Sleep(3 * time.Second)
	}
	defer conn.Close()

	// Suggest CrowdSec (skip if non-interactive)
	if !f.crowdsec && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("  ? Install CrowdSec (WAF/IPS)? [Y/n]: ")
		var resp string
		fmt.Scanln(&resp)
		if resp == "" || resp == "y" || resp == "Y" || resp == "yes" {
			f.crowdsec = true
		}
	}
	if f.crowdsec {
		if err := installCrowdSec(conn); err != nil {
			return err
		}
	}

	// Phase 2: Docker (if not bare)
	if f.mode == "docker" || f.mode == "k3s" {
		if err := docker.Install(conn); err != nil {
			return err
		}
	}

	// Phase 3: k3s
	if f.mode == "k3s" {
		installCfg := k3s.DefaultInstallConfig(ip)
		installCfg.LocalPath = f.kubeconfig
		installCfg.Context = f.contextName
		installCfg.Merge = f.mergeConfig
		installCfg.DisableTraefik = f.disableTraefik

		if err := k3s.Install(conn, installCfg); err != nil {
			return err
		}
	}

	// Phase 4: Log shipping (Promtail)
	if f.logsURL != "" {
		if err := deploy.InstallPromtail(conn, deploy.PromtailConfig{
			LokiURL:  f.logsURL,
			NodeName: ip,
		}); err != nil {
			return fmt.Errorf("promtail: %w", err)
		}
	}

	// Phase 5: Alerting (Alertmanager)
	if f.alertsURL != "" {
		if err := deploy.InstallAlertmanager(conn, deploy.AlertmanagerConfig{
			SlackWebhookURL: f.alertsURL,
		}); err != nil {
			return fmt.Errorf("alertmanager: %w", err)
		}
	}

	// Create /opt/sdk-ops/ structure
	fmt.Println("  → Creating /opt/sdk-ops/ structure...")
	ssh.Run(conn, `mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs && echo "sdk-ops-init" > /opt/sdk-ops/.version`)

	// Auto-register node in ~/.sdk-ops/config.yaml
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
			User: hardCfg.User,
			Key:  f.key,
			Port: hardCfg.SSHPort,
			Mode: f.mode,
		})
		saveConfig(cfg)
		fmt.Printf("  → Registered node in %s\n", configPath())
	}

	fmt.Println("\n✅ infra init complete!")
	fmt.Printf("   SSH: ssh %s@%s -p %d\n", hardCfg.User, ip, hardCfg.SSHPort)
	if f.mode == "k3s" {
		fmt.Printf("   Kubeconfig: %s\n", f.kubeconfig)
	}
	return nil
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
	defer agentConn.Close()

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
		defer serverConn.Close()
	}

	joinCfg := k3s.JoinConfig{
		ServerIP: serverIP,
		Token:    token,
	}
	if err := k3s.Join(agentConn, serverConn, joinCfg); err != nil {
		return err
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
	defer conn.Close()

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

func runInfraRemove(ip string, f infraFlags) error {
	client := infraSSHClient(ip, f.user, f.port, f)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer conn.Close()

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
	fmt.Scanln(&resp)
	if resp != "y" && resp != "Y" && resp != "yes" {
		fmt.Println("  Aborted.")
		return nil
	}

	scripts := []string{
		"k3s-uninstall.sh",
		"/usr/local/bin/k3s-killall.sh",
	}
	for _, s := range scripts {
		ssh.Run(conn, fmt.Sprintf("test -f %s && %s || true", s, s))
	}

	ssh.Run(conn, `apt-get remove -y docker-ce docker-ce-cli containerd.io docker-compose-plugin 2>/dev/null || true`)
	ssh.Run(conn, `rm -rf /opt/sdk-ops`)
	ssh.Run(conn, `rm -f /etc/sudoers.d/sdk-ops`)

	fmt.Println("✅ sdk-ops removed from", ip)
	return nil
}
