package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/providers"
	"github.com/natuleadan/sdk-ops/terraform"
)

type providerFlags struct {
	provider  string
	plan      string
	location  string
	template  string
	hostname  string
	sshKeyIDs string
	apiKey    string
	projectID int
	name      string
	version   string
	nodePlan  string
	nodeCount int
}

var pf providerFlags
var sshKeyPub string

func newProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage cloud provider resources (VPS, K8s, LB, DNS)",
	}

	cmd.AddCommand(newProviderVPSCmd())
	cmd.AddCommand(newProviderK8sCmd())
	cmd.AddCommand(newProviderLBCmd())
	cmd.AddCommand(newProviderDNSCmd())
	cmd.AddCommand(newProviderSSHKeyCmd())

	cmd.PersistentFlags().StringVar(&pf.provider, "provider", "", "Provider name (cubepath, hetzner, digitalocean, vultr, aws)")
	cmd.PersistentFlags().StringVar(&pf.apiKey, "api-key", "", "API key (or provider-specific env var)")
	cmd.PersistentFlags().IntVar(&pf.projectID, "project-id", 0, "Project ID for provider")

	return cmd
}

func getProvider() (providers.Provider, error) {
	apiKey := pf.apiKey

	switch pf.provider {
	case "cubepath":
		return newCubePathProvider(apiKey, pf.projectID)
	case "hetzner":
		return newHetznerProvider(apiKey)
	case "digitalocean":
		return newDigitalOceanProvider(apiKey)
	case "vultr":
		return newVultrProvider(apiKey)
	case "aws":
		return newAWSProvider()
	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: cubepath, hetzner, digitalocean, vultr, aws)", pf.provider)
	}
}

// --- VPS ---

func newProviderVPSCmd() *cobra.Command {
	var cfg providers.VPSCreateConfig

	cmd := &cobra.Command{
		Use:   "vps",
		Short: "Manage VPS instances",
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new VPS",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			cfg.Label = pf.hostname
			cfg.Plan = pf.plan
			cfg.Location = pf.location
			cfg.Template = pf.template
			cfg.Hostname = pf.hostname
			cfg.EnableIPv4, _ = cmd.Flags().GetBool("ipv4")
			cfg.EnableIPv6, _ = cmd.Flags().GetBool("ipv6")
			if pf.sshKeyIDs != "" {
				for s := range strings.SplitSeq(pf.sshKeyIDs, ",") {
					var id int
					if _, err := fmt.Sscanf(s, "%d", &id); err != nil { log.Printf("provider: parse id error: %v", err) }
					if id > 0 {
						cfg.SSHKeyIDs = append(cfg.SSHKeyIDs, id)
					}
				}
			}
			vps, err := p.CreateVPS(context.Background(), cfg)
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s @ %s (%s)\n", vps.ID, vps.Name, vps.IP, vps.Status)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List VPS instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			list, err := p.ListVPS(context.Background())
			if err != nil {
				return err
			}
			for _, v := range list {
				fmt.Printf("[%s] %s @ %s (%s)\n", v.ID, v.Name, v.IP, v.Status)
			}
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a VPS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteVPS(context.Background(), args[0])
		},
	}

	createCmd.Flags().StringVar(&pf.plan, "plan", "", "VPS plan")
	createCmd.Flags().StringVar(&pf.location, "location", "", "Location")
	createCmd.Flags().StringVar(&pf.template, "template", "", "OS template")
	createCmd.Flags().StringVar(&pf.hostname, "hostname", "", "Hostname")
	createCmd.Flags().StringVar(&pf.sshKeyIDs, "ssh-key-ids", "", "SSH key IDs (comma-separated)")
	createCmd.Flags().Bool("ipv4", true, "Enable public IPv4")
	createCmd.Flags().Bool("ipv6", true, "Enable public IPv6")

	exportCmd := &cobra.Command{
		Use:   "export <id>",
		Short: "Export VPS as Terraform HCL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			vps, err := p.GetVPS(context.Background(), args[0])
			if err != nil {
				return err
			}
			hcl := terraform.ExportVPS(pf.provider, *vps)
			fmt.Println(hcl)
			return nil
		},
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(listCmd)
	cmd.AddCommand(deleteCmd)
	cmd.AddCommand(exportCmd)
	return cmd
}

// --- K8s ---

func newProviderK8sCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "k8s",
		Short: "Manage Kubernetes clusters",
	}

	cmd.AddCommand(newProviderK8sCreateCmd())
	cmd.AddCommand(newProviderK8sListCmd())
	cmd.AddCommand(newProviderK8sDeleteCmd())
	cmd.AddCommand(newProviderK8sKubeconfigCmd())
	cmd.AddCommand(newProviderK8sUpdateCmd())
	cmd.AddCommand(newProviderK8sProtectionCmd())

	addonsCmd := newProviderK8sAddonsCmd()
	cmd.AddCommand(addonsCmd)

	nodePoolCmd := newProviderK8sNodePoolCmd()
	cmd.AddCommand(nodePoolCmd)

	k8sLBListCmd := newProviderK8sLBListCmd()
	cmd.AddCommand(k8sLBListCmd)

	return cmd
}

func newProviderK8sCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a K8s cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			cfg := providers.K8sCreateConfig{
				Name:      pf.name,
				Location:  pf.location,
				Version:   pf.version,
				NodePlan:  pf.nodePlan,
				NodeCount: pf.nodeCount,
			}
			cluster, err := p.CreateK8s(context.Background(), cfg)
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s (%s) - %d nodes\n", cluster.ID, cluster.Name, cluster.Status, cluster.NodeCount)
			return nil
		},
	}

	cmd.Flags().StringVar(&pf.name, "name", "", "Cluster name")
	cmd.Flags().StringVar(&pf.location, "location", "", "Location")
	cmd.Flags().StringVar(&pf.version, "version", "", "K8s version")
	cmd.Flags().StringVar(&pf.nodePlan, "node-plan", "", "Node plan")
	cmd.Flags().IntVar(&pf.nodeCount, "nodes", 3, "Number of nodes")
	return cmd
}

func newProviderK8sListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List K8s clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			list, err := p.ListK8s(context.Background())
			if err != nil {
				return err
			}
			for _, c := range list {
				fmt.Printf("[%s] %s (%s) - %d nodes\n", c.ID, c.Name, c.Status, c.NodeCount)
			}
			return nil
		},
	}
}

func newProviderK8sDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a K8s cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteK8s(context.Background(), args[0])
		},
	}
}

func newProviderK8sKubeconfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kubeconfig <id>",
		Short: "Get K8s kubeconfig",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			kc, err := p.GetKubeconfig(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Print(kc)
			return nil
		},
	}
}

func newProviderK8sUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Upgrade K8s version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			version, _ := cmd.Flags().GetString("version")
			if version == "" {
				return fmt.Errorf("--version is required")
			}
			cl, err := p.UpdateK8s(context.Background(), args[0], version)
			if err != nil {
				return err
			}
			fmt.Printf("[%s] upgraded to %s (%s)\n", cl.ID, cl.Version, cl.Status)
			return nil
		},
	}
	cmd.Flags().String("version", "", "Target K8s version")
	return cmd
}

func newProviderK8sProtectionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "protection <id>",
		Short: "Toggle K8s cluster deletion protection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			cl, err := p.ToggleK8sProtection(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("[%s] protection toggled (%s)\n", cl.ID, cl.Status)
			return nil
		},
	}
}

func newProviderK8sAddonsCmd() *cobra.Command {
	addonsCmd := &cobra.Command{Use: "addons", Short: "Manage K8s addons"}

	addonsListCmd := &cobra.Command{
		Use: "list <id>", Short: "List installed addons", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			list, err := p.ListK8sAddons(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, a := range list {
				fmt.Printf("[%s] %s (%s) v%s\n", a.ID, a.Name, a.Status, a.Version)
			}
			return nil
		},
	}
	addonsAvailableCmd := &cobra.Command{
		Use: "available", Short: "List available addons",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			list, err := p.ListAvailableAddons(context.Background())
			if err != nil {
				return err
			}
			for _, a := range list {
				fmt.Printf("[%s] %s (%s)\n", a.Slug, a.Name, a.Version)
			}
			return nil
		},
	}
	addonsInstallCmd := &cobra.Command{
		Use: "install <id> <slug>", Short: "Install an addon", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.InstallK8sAddon(context.Background(), args[0], args[1])
		},
	}
	addonsUninstallCmd := &cobra.Command{
		Use: "uninstall <id> <addon-id>", Short: "Uninstall an addon", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.UninstallK8sAddon(context.Background(), args[0], args[1])
		},
	}
	addonsCmd.AddCommand(addonsListCmd)
	addonsCmd.AddCommand(addonsAvailableCmd)
	addonsCmd.AddCommand(addonsInstallCmd)
	addonsCmd.AddCommand(addonsUninstallCmd)
	return addonsCmd
}

func newProviderK8sNodePoolCmd() *cobra.Command {
	nodePoolCmd := &cobra.Command{Use: "node-pool", Short: "Manage K8s node pools"}

	npListCmd := &cobra.Command{
		Use: "list <id>", Short: "List node pools", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			pools, err := p.ListK8sNodePools(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, po := range pools {
				fmt.Printf("[%s] %s plan=%s nodes=%d (%s)\n", po.ID, po.Name, po.Plan, po.Nodes, po.Status)
			}
			return nil
		},
	}
	npAddCmd := &cobra.Command{
		Use: "add <id>", Short: "Add a node pool",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			plan, _ := cmd.Flags().GetString("plan")
			nodes, _ := cmd.Flags().GetInt("nodes")
			pool, err := p.CreateK8sNodePool(context.Background(), args[0], providers.K8sNodePoolConfig{
				Plan: plan, NodeCount: nodes,
			})
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s (%d nodes)\n", pool.ID, pool.Plan, pool.Nodes)
			return nil
		},
	}
	npAddCmd.Flags().String("plan", "", "Node plan")
	npAddCmd.Flags().Int("nodes", 1, "Number of nodes")
	npScaleCmd := &cobra.Command{
		Use: "scale <id> <pool-id>", Short: "Scale a node pool", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			nodes, _ := cmd.Flags().GetInt("nodes")
			return p.ScaleK8sNodePool(context.Background(), args[0], args[1], nodes)
		},
	}
	npScaleCmd.Flags().Int("nodes", 1, "Number of nodes")
	npDeleteCmd := &cobra.Command{
		Use: "delete <id> <pool-id>", Short: "Delete a node pool", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteK8sNodePool(context.Background(), args[0], args[1])
		},
	}
	nodePoolCmd.AddCommand(npListCmd)
	nodePoolCmd.AddCommand(npAddCmd)
	nodePoolCmd.AddCommand(npScaleCmd)
	nodePoolCmd.AddCommand(npDeleteCmd)
	return nodePoolCmd
}

func newProviderK8sLBListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "lb-list <id>", Short: "List LBs attached to a K8s cluster", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			list, err := p.ListK8sLBs(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, lb := range list {
				fmt.Printf("[%s] %s @ %s\n", lb.ID, lb.Name, lb.IP)
			}
			return nil
		},
	}
}

// --- LB ---

func newProviderLBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lb",
		Short: "Manage Load Balancers",
	}

	cmd.AddCommand(newProviderLBCreateCmd())
	cmd.AddCommand(newProviderLBListCmd())
	cmd.AddCommand(newProviderLBDeleteCmd())

	listenerCmd := newProviderLBListenerCmd()
	cmd.AddCommand(listenerCmd)

	healthCmd := newProviderLBHealthCmd()
	cmd.AddCommand(healthCmd)

	targetCmd := newProviderLBTargetCmd()
	cmd.AddCommand(targetCmd)

	cmd.AddCommand(newProviderLBResizeCmd())
	cmd.AddCommand(newProviderLBMetricsCmd())
	cmd.AddCommand(newProviderLBProtectionCmd())

	return cmd
}

func newProviderLBCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a load balancer",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			cfg := providers.LBCreateConfig{
				Name:     pf.name,
				Location: pf.location,
				Plan:     pf.plan,
			}
			lb, err := p.CreateLB(context.Background(), cfg)
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s @ %s (%s)\n", lb.ID, lb.Name, lb.IP, lb.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&pf.name, "name", "", "LB name")
	cmd.Flags().StringVar(&pf.location, "location", "", "Location")
	cmd.Flags().StringVar(&pf.plan, "plan", "", "LB plan")
	return cmd
}

func newProviderLBListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List load balancers",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			list, err := p.ListLB(context.Background())
			if err != nil {
				return err
			}
			for _, lb := range list {
				fmt.Printf("[%s] %s @ %s (%s)\n", lb.ID, lb.Name, lb.IP, lb.Status)
			}
			return nil
		},
	}
}

func newProviderLBDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a load balancer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteLB(context.Background(), args[0])
		},
	}
}

func newProviderLBListenerCmd() *cobra.Command {
	listenerCmd := &cobra.Command{Use: "listener", Short: "Manage LB listeners"}

	lsAddCmd := &cobra.Command{
		Use: "add <lb-id>", Short: "Add a listener", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			port, _ := cmd.Flags().GetInt("port")
			targetPort, _ := cmd.Flags().GetInt("target-port")
			listener, err := p.CreateLBListener(context.Background(), args[0], providers.LBListenerConfig{
				Port: port, TargetPort: targetPort,
			})
			if err != nil {
				return err
			}
			fmt.Printf("[%s] :%d → :%d\n", listener.ID, listener.Port, listener.TargetPort)
			return nil
		},
	}
	lsAddCmd.Flags().Int("port", 80, "Listener port")
	lsAddCmd.Flags().Int("target-port", 8080, "Target port")
	lsUpdateCmd := &cobra.Command{
		Use: "update <lb-id> <listener-id>", Short: "Update a listener", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			port, _ := cmd.Flags().GetInt("port")
			targetPort, _ := cmd.Flags().GetInt("target-port")
			_, err = p.UpdateLBListener(context.Background(), args[0], args[1], providers.LBListenerConfig{
				Port: port, TargetPort: targetPort,
			})
			return err
		},
	}
	lsUpdateCmd.Flags().Int("port", 0, "Listener port")
	lsUpdateCmd.Flags().Int("target-port", 0, "Target port")
	lsDeleteCmd := &cobra.Command{
		Use: "delete <lb-id> <listener-id>", Short: "Delete a listener", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteLBListener(context.Background(), args[0], args[1])
		},
	}
	listenerCmd.AddCommand(lsAddCmd)
	listenerCmd.AddCommand(lsUpdateCmd)
	listenerCmd.AddCommand(lsDeleteCmd)
	return listenerCmd
}

func newProviderLBHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "health-check <lb-id> <listener-id>", Short: "Set LB health check",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			path, _ := cmd.Flags().GetString("path")
			return p.SetLBHealthCheck(context.Background(), args[0], args[1],
				providers.LBHealthCheckConfig{Path: path})
		},
	}
	cmd.Flags().String("path", "/health", "Health check path")
	return cmd
}

func newProviderLBTargetCmd() *cobra.Command {
	targetCmd := &cobra.Command{Use: "target", Short: "Manage LB targets"}
	tgtAddCmd := &cobra.Command{
		Use: "add <lb-id> <listener-id>", Short: "Add a target", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			targetType, _ := cmd.Flags().GetString("type")
			targetID, _ := cmd.Flags().GetString("uuid")
			port, _ := cmd.Flags().GetInt("port")
			tgt, err := p.AddLBTarget(context.Background(), args[0], args[1],
				providers.LBTargetConfig{Type: targetType, TargetID: targetID, Port: port})
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s (%s:%d)\n", tgt.ID, tgt.Type, tgt.TargetID, tgt.Port)
			return nil
		},
	}
	tgtAddCmd.Flags().String("type", "vps", "Target type: vps, ip, baremetal")
	tgtAddCmd.Flags().String("uuid", "", "Target UUID")
	tgtAddCmd.Flags().Int("port", 8080, "Target port")
	tgtListCmd := &cobra.Command{
		Use: "list <lb-id> <listener-id>", Short: "List targets", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			tgts, err := p.ListLBTargets(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			for _, t := range tgts {
				fmt.Printf("[%s] %s → %s:%d (%s)\n", t.ID, t.Type, t.TargetID, t.Port, t.Status)
			}
			return nil
		},
	}
	tgtDrainCmd := &cobra.Command{
		Use: "drain <lb-id> <listener-id> <target-id>", Short: "Drain a target", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DrainLBTarget(context.Background(), args[0], args[1], args[2])
		},
	}
	targetCmd.AddCommand(tgtAddCmd)
	targetCmd.AddCommand(tgtListCmd)
	targetCmd.AddCommand(tgtDrainCmd)
	return targetCmd
}

func newProviderLBResizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "resize <lb-id>", Short: "Resize LB plan",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			plan, _ := cmd.Flags().GetString("plan")
			lb, err := p.ResizeLB(context.Background(), args[0], plan)
			if err != nil {
				return err
			}
			fmt.Printf("[%s] resized to %s\n", lb.ID, lb.Plan)
			return nil
		},
	}
	cmd.Flags().String("plan", "", "Target plan (e.g. lb.medium)")
	return cmd
}

func newProviderLBMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use: "metrics <lb-id>", Short: "Show LB metrics",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			m, err := p.GetLBMetrics(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Println(m)
			return nil
		},
	}
}

func newProviderLBProtectionCmd() *cobra.Command {
	return &cobra.Command{
		Use: "protection <lb-id>", Short: "Toggle LB deletion protection",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			lb, err := p.ToggleLBProtection(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("[%s] protection toggled\n", lb.ID)
			return nil
		},
	}
}

// --- DNS ---

func newProviderDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Manage DNS zones and records",
	}

	listCmd := &cobra.Command{
		Use:   "list-zones",
		Short: "List DNS zones",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			zones, err := p.ListDNSZones(context.Background())
			if err != nil {
				return err
			}
			for _, z := range zones {
				fmt.Printf("[%s] %s\n", z.ID, z.Name)
			}
			return nil
		},
	}

	addRecordCmd := &cobra.Command{
		Use:   "add-record <zone-id> <type> <name> <value>",
		Short: "Add a DNS record",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			r := providers.DNSRecord{
				Type:  args[1],
				Name:  args[2],
				Value: args[3],
			}
			return p.CreateDNSRecord(context.Background(), args[0], r)
		},
	}

	deleteRecordCmd := &cobra.Command{
		Use:   "delete-record <zone-id> <record-id>",
		Short: "Delete a DNS record",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteDNSRecord(context.Background(), args[0], args[1])
		},
	}

	cmd.AddCommand(listCmd)
	cmd.AddCommand(addRecordCmd)
	cmd.AddCommand(deleteRecordCmd)
	return cmd
}

// --- SSH Keys ---

func newProviderSSHKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh-key",
		Short: "Manage SSH keys on the provider",
	}

	uploadCmd := &cobra.Command{
		Use:   "upload <name>",
		Short: "Upload an SSH public key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			pubKey := sshKeyPub
			if pubKey == "" {
				homeDir, _ := os.UserHomeDir()
				pubKey = filepath.Join(homeDir, ".ssh", "id_ed25519.pub")
			}
			data, err := os.ReadFile(filepath.Clean(pubKey))
			if err != nil {
				return fmt.Errorf("read key: %w", err)
			}
			key, err := p.CreateSSHKey(context.Background(), providers.SSHKeyCreateConfig{
				Name:      args[0],
				PublicKey: string(data),
			})
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s (%s)\n", key.ID, key.Name, key.Fingerprint)
			return nil
		},
	}
	uploadCmd.Flags().StringVar(&sshKeyPub, "pub-key", "", "Path to public key file (default: ~/.ssh/id_ed25519.pub)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List SSH keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			keys, err := p.ListSSHKeys(context.Background())
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				fmt.Println("  No SSH keys found")
				return nil
			}
			for _, k := range keys {
				fmt.Printf("[%s] %s (%s)\n", k.ID, k.Name, k.Fingerprint)
			}
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an SSH key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProvider()
			if err != nil {
				return err
			}
			return p.DeleteSSHKey(context.Background(), args[0])
		},
	}

	cmd.AddCommand(uploadCmd)
	cmd.AddCommand(listCmd)
	cmd.AddCommand(deleteCmd)
	return cmd
}
