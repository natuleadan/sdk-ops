package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/bunny"
	"github.com/natuleadan/sdk-ops/providers"
)

type bunnyFlags struct {
	apiKey     string
	region     string
	minInst    int32
	maxInst    int32
	port       int32
	volumeName string
	volumeSize int32
	volumePath string
	env        string
	recordType string
	ttl        int32
	priority   int32
	origin     string
	hostname   string
	registryID string
	digest     string
	anycast    bool
	endpointPort int32
}

var bf bunnyFlags

var regionAliases = map[string]string{
	"bogota":   "CO",
	"colombia": "CO",
	"latam":    "CO",
	"miami":    "MI",
	"usa":      "MI",
	"us":       "MI",
	"na":       "MI",
	"frankfurt": "DE",
	"germany":  "DE",
	"europe":   "DE",
	"eu":       "DE",
	"london":   "UK",
	"uk":       "UK",
	"tokyo":    "JP",
	"japan":    "JP",
	"singapore": "SG",
	"sg":       "SG",
	"sydney":   "SYD",
	"brazil":   "BR",
	"mexico":   "MX",
	"ny":       "NY",
	"newyork":  "NY",
	"la":       "LA",
	"losangeles": "LA",
	"dallas":   "TX",
	"texas":    "TX",
	"paris":    "FR",
	"france":   "FR",
	"spain":    "ES",
	"italy":    "IT",
	"poland":   "PL",
	"sweden":   "SE",
}

func resolveRegion(s string) string {
	if s == "" {
		return "CO"
	}
	if r, ok := regionAliases[strings.ToLower(s)]; ok {
		return r
	}
	return strings.ToUpper(s)
}

func newBunnyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bunny",
		Short: "Manage bunny.net services (CDN, DNS, Magic Containers, Storage)",
		Long: `Manage bunny.net services via the REST API.

Requires BUNNY_API_KEY environment variable or --api-key flag.

Region aliases:
  bogota, colombia, latam     → CO (default)
  miami, usa, us, na          → MI
  frankfurt, germany, europe  → DE
  london, uk                  → UK
  tokyo, japan                → JP
  singapore                   → SG`,
	}

	cmd.PersistentFlags().StringVar(&bf.apiKey, "api-key", "", "bunny.net API key (or BUNNY_API_KEY env)")

	cmd.AddCommand(newBunnyAppCmd())
	cmd.AddCommand(newBunnyDNSCmd())
	cmd.AddCommand(newBunnyPullZoneCmd())
	cmd.AddCommand(newBunnyScriptCmd())
	cmd.AddCommand(newBunnyStorageCmd())
	cmd.AddCommand(newBunnyStreamCmd())
	cmd.AddCommand(newBunnyShieldCmd())
	cmd.AddCommand(newBunnyLoginCmd())

	return cmd
}

func bunnyClient() *bunny.Client {
	key := bf.apiKey
	if key == "" {
		key = os.Getenv("BUNNY_API_KEY")
	}
	if key == "" {
		if creds, err := providers.LoadCredentials(); err == nil {
			key = creds.BunnyAPIKey
		}
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "Error: BUNNY_API_KEY not set. Use --api-key flag or set BUNNY_API_KEY env var.")
		os.Exit(1)
	}
	return bunny.NewClient(key)
}

// --- bunny login ---

func newBunnyLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Verify API key and show account info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			_, err := c.ListDNSZones(ctx)
			if err != nil {
				return fmt.Errorf("auth failed: %w", err)
			}
			fmt.Println("✓ API key is valid")
			return nil
		},
	}
}

// ============================================================================
// bunny app subcommands
// ============================================================================

func newBunnyAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage Magic Containers applications",
	}

	cmd.AddCommand(newBunnyAppCreateCmd())
	cmd.AddCommand(newBunnyAppListCmd())
	cmd.AddCommand(newBunnyAppStatusCmd())
	cmd.AddCommand(newBunnyAppDeleteCmd())
	cmd.AddCommand(newBunnyAppDeployCmd())
	cmd.AddCommand(newBunnyAppRestartCmd())
	cmd.AddCommand(newBunnyAppEndpointCmd())
	cmd.AddCommand(newBunnyAppLogsCmd())
	cmd.AddCommand(newBunnyAppOverviewCmd())

	return cmd
}

func newBunnyAppLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <app-id>",
		Short: "Show recent container logs and pod status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			ov, err := c.GetAppOverview(ctx, args[0])
			if err != nil {
				return err
			}
			for _, r := range ov.Regions {
				ramMB := r.AverageRAM / 1024 / 1024
				fmt.Printf("Region: %s  instances=%d  status=%s  cpu=%.1f%%  ram=%.0fMB  req=%.0f\n",
					r.Region, r.Instances, r.Status, r.AverageCPU, ramMB, r.Requests)
				for _, p := range r.Pods {
					podRAMMB := p.RAMUsage / 1024 / 1024
					fmt.Printf("  Pod: %s  status=%s  cpu=%.1f%%  ram=%.0fMB\n",
						p.Name, p.Status, p.CPUUsage, podRAMMB)
					for _, ct := range p.Containers {
						restarts := 0
						if ct.NumberOfRestarts != nil {
							restarts = int(*ct.NumberOfRestarts)
						}
						msg := ""
						if ct.Message != "" {
							msg = fmt.Sprintf("  msg: %s", ct.Message)
						}
						fmt.Printf("    Container: %s  status=%s  restarts=%d%s\n",
							ct.Name, ct.Status, restarts, msg)
					}
				}
			}
			return nil
		},
	}
}

func newBunnyAppOverviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "overview <app-id>",
		Short: "Show application resource usage and cost",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			ov, err := c.GetAppOverview(ctx, args[0])
			if err != nil {
				return err
			}
			summary, _ := c.GetAppUsageSummary(ctx, args[0])

			fmt.Printf("Status:     %s\n", ov.Status)
			fmt.Printf("Instances:  %d active / %d desired\n", safeDerefInt32(ov.ActiveInstances).Indicator, ov.DesiredInstances)
			fmt.Printf("Regions:    %d active\n", safeDerefInt32(ov.ActiveRegions).Indicator)
			fmt.Printf("Monthly:    $%.2f\n", ov.MonthlyCost)
			if summary != nil {
				fmt.Printf("Avg Cost:   $%.2f/month\n", summary.MonthlyCost)
				fmt.Printf("Avg Lat:    %.0fms\n", summary.AverageLatency)
			}
			if ov.AverageCPU != nil {
				fmt.Printf("CPU:        %.1f%%\n", ov.AverageCPU.Indicator)
			}
			if ov.AverageRAM != nil {
				ramVal := ov.AverageRAM.Indicator
				switch {
				case ramVal > 1e9:
					fmt.Printf("RAM:        %.1f GB\n", ramVal/1e9)
				case ramVal > 1e6:
					fmt.Printf("RAM:        %.1f MB\n", ramVal/1e6)
				default:
					fmt.Printf("RAM:        %.0f%%\n", ramVal)
				}
			}
			curLat := 0.0
			tgtLat := 0.0
			if ov.CurrentLatency != nil {
				curLat = ov.CurrentLatency.Indicator
			}
			if ov.TargetLatency != nil {
				tgtLat = ov.TargetLatency.Indicator
			}
			fmt.Printf("Latency:    %.0fms (current) / %.0fms (target)\n", curLat, tgtLat)
			if ov.TotalVolumeSizeInGB > 0 {
				fmt.Printf("Volumes:    %.1f GB\n", ov.TotalVolumeSizeInGB)
			}
			return nil
		},
	}
}

func safeDerefInt32(v *bunny.Int32StatusIndicator) bunny.Int32StatusIndicator {
	if v == nil {
		return bunny.Int32StatusIndicator{}
	}
	return *v
}

func newBunnyAppEndpointCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoint",
		Short: "Manage application endpoints",
	}

	addAnycast := &cobra.Command{
		Use:   "add-anycast <app-id>",
		Short: "Add an Anycast IP endpoint (direct pod access, bypasses CDN)",
		Args:  cobra.ExactArgs(1),
		RunE:  runBunnyAppEndpointAddAnycast,
	}
	addAnycast.Flags().Int32VarP(&bf.endpointPort, "port", "p", 3000, "Container port to expose via Anycast")

	cmd.AddCommand(addAnycast)

	return cmd
}

func runBunnyAppEndpointAddAnycast(cmd *cobra.Command, args []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	port := bf.endpointPort
	if port == 0 {
		port = 3000
	}

	req := bunny.EndpointRequest{
		DisplayName: "Anycast-Bench",
		Anycast: &bunny.AnycastEndpointRequest{
			Type: bunny.AnycastIPv4,
			PortMappings: []bunny.ContainerPortMappingRequest{
				{
					ContainerPort: port,
					ExposedPort:   &port,
					Protocols:     []bunny.Protocol{bunny.ProtoTCP},
				},
			},
		},
	}

	app, err := c.GetApp(ctx, args[0])
	if err != nil {
		return fmt.Errorf("get app: %w", err)
	}
	if len(app.ContainerTemplates) == 0 {
		return fmt.Errorf("no containers in app %s", args[0])
	}
	containerID := app.ContainerTemplates[0].ID

	resp, err := c.AddEndpoint(ctx, args[0], containerID, req)
	if err != nil {
		return err
	}
	fmt.Printf("✓ Anycast endpoint added: %s\n", resp.ID)

	ep, err := c.ListEndpoints(ctx, args[0])
	if err == nil {
		for _, e := range ep.Items {
			if e.ID == resp.ID {
				if len(e.InternalIPAddresses) > 0 {
					fmt.Printf("  Internal IP: %s (%s)\n", e.InternalIPAddresses[0].Address, e.InternalIPAddresses[0].Region)
				}
				if len(e.PublicIPAddresses) > 0 {
					fmt.Printf("  Public IP: %s (%s)\n", e.PublicIPAddresses[0].Address, e.PublicIPAddresses[0].Region)
				}
			}
		}
	}
	return nil
}

func newBunnyAppCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new Magic Containers application",
		Long: `Create a new application on Magic Containers.

The app is deployed in a single region by default (CO - Bogotá, LATAM).
Use --region to change it, or a region alias like --region miami or --region eu.

Examples:
  sdk-ops bunny app create my-api --image library/nginx:alpine --port 80
  sdk-ops bunny app create my-api --image ghcr.io/user/app:latest --port 3000 --region miami --min 2 --max 5
  sdk-ops bunny app create my-api --image library/redis:7-alpine --port 6379 --region eu --volume-name data --volume-path /data`,
		Args: cobra.ExactArgs(1),
		RunE: runBunnyAppCreate,
	}
	cmd.Flags().StringVarP(&bf.origin, "image", "i", "", "Container image (e.g. library/nginx:alpine or ghcr.io/user/repo:tag)")
	cmd.Flags().Int32VarP(&bf.port, "port", "p", 8080, "Container port to expose")
	cmd.Flags().StringVarP(&bf.region, "region", "r", "CO", "Deploy region (ID or alias: bogota, miami, eu, sg, tokyo...)")
	cmd.Flags().Int32VarP(&bf.minInst, "min", "m", 1, "Minimum instances (pods)")
	cmd.Flags().Int32VarP(&bf.maxInst, "max", "M", 1, "Maximum instances (autoscaling ceiling)")
	cmd.Flags().StringVarP(&bf.env, "env", "e", "", "Comma-separated KEY=VALUE env vars")
	cmd.Flags().StringVar(&bf.volumeName, "volume-name", "", "Persistent volume name")
	cmd.Flags().Int32Var(&bf.volumeSize, "volume-size", 5, "Volume size in GB")
	cmd.Flags().StringVar(&bf.volumePath, "volume-path", "", "Volume mount path (e.g. /data)")
	cmd.Flags().StringVar(&bf.registryID, "registry-id", "", "Container registry ID (overrides auto-detection)")
	cmd.Flags().StringVar(&bf.digest, "digest", "", "Image digest (sha256:..., required for private images)")
	cmd.Flags().BoolVar(&bf.anycast, "anycast", false, "Use Anycast IP instead of CDN endpoint (direct pod access)")
	_ = cmd.MarkFlagRequired("image")
	return cmd
}

func runBunnyAppCreate(cmd *cobra.Command, args []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	region := resolveRegion(bf.region)

	envMap := make(map[string]string)
	for _, e := range strings.Split(bf.env, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	opts := bunny.DeployOptions{
		AppName:      args[0],
		Regions:      []string{region},
		Image:        bf.origin,
		ImageDigest:  bf.digest,
		Port:         bf.port,
		MinInstances: bf.minInst,
		MaxInstances: bf.maxInst,
		Env:          envMap,
		VolumeName:   bf.volumeName,
		VolumeSize:   bf.volumeSize,
		VolumePath:   bf.volumePath,
		RegistryID:   bf.registryID,
		Anycast:      bf.anycast,
	}

	fmt.Printf("Deploying %s to %s (%d-%d instances)...\n", args[0], region, bf.minInst, bf.maxInst)

	resp, err := c.DeployFromImage(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Printf("✓ App created: %s\n", resp.ID)
	return nil
}

func newBunnyAppListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all applications",
		Args:  cobra.NoArgs,
		RunE:  runBunnyAppList,
	}
}

func runBunnyAppList(cmd *cobra.Command, _ []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	resp, err := c.ListApps(ctx, "", 100)
	if err != nil {
		return err
	}
	if len(resp.Items) == 0 {
		fmt.Println("No applications found.")
		return nil
	}
	for _, app := range resp.Items {
		ep := ""
		if app.DisplayEndpoint != nil {
			ep = app.DisplayEndpoint.Address
		}
		fmt.Printf("%s  %s  [%s]  %s\n", app.ID, app.Name, app.Status, ep)
	}
	return nil
}

func newBunnyAppStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <app-id>",
		Short: "Show application details and overview",
		Args:  cobra.ExactArgs(1),
		RunE:  runBunnyAppStatus,
	}
}

func runBunnyAppStatus(cmd *cobra.Command, args []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	app, err := c.GetApp(ctx, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("ID:       %s\n", app.ID)
	fmt.Printf("Name:     %s\n", app.Name)
	fmt.Printf("Status:   %s\n", app.Status)
	fmt.Printf("Type:     %s\n", app.RuntimeType)
	if app.DisplayEndpoint != nil {
		fmt.Printf("URL:      %s\n", app.DisplayEndpoint.Address)
	}
	fmt.Printf("Regions:  %v\n", app.RegionSettings.AllowedRegionIDs)
	if app.AutoScaling != nil {
		fmt.Printf("Scaling:  %d-%d instances\n", app.AutoScaling.Min, app.AutoScaling.Max)
	}
	fmt.Printf("Containers: %d\n", len(app.ContainerTemplates))
	for _, ct := range app.ContainerTemplates {
		fmt.Printf("  - %s: %s/%s:%s\n", ct.Name, ct.ImageNamespace, ct.ImageName, ct.ImageTag)
		for _, ep := range ct.Endpoints {
			fmt.Printf("    Endpoint: %s (%s)\n", ep.PublicHost, ep.Type)
		}
	}

	overview, err := c.GetAppOverview(ctx, args[0])
	if err == nil {
		fmt.Printf("\nOverview:\n")
		fmt.Printf("  Monthly Cost:   $%.2f\n", overview.MonthlyCost)
		fmt.Printf("  Avg Latency:    %.0fms\n", overview.AverageLatency)
		fmt.Printf("  Active Inst:    %d\n", overview.DesiredInstances)
		if overview.AverageCPU != nil {
			cpuVal := overview.AverageCPU.Indicator
			unit := "%"
			if cpuVal > 100 {
				unit = "m"
			}
			fmt.Printf("  Avg CPU:        %.0f%s\n", cpuVal, unit)
		}
		if overview.AverageRAM != nil {
			ramVal := overview.AverageRAM.Indicator
			ramDisplay := ramVal
			unit := "%"
			if ramVal > 1e9 {
				ramDisplay = ramVal / 1e9
				unit = "GB"
			} else if ramVal > 1e6 {
				ramDisplay = ramVal / 1e6
				unit = "MB"
			}
			fmt.Printf("  Avg RAM:        %.0f%s\n", ramDisplay, unit)
		}
	}
	return nil
}

func newBunnyAppDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <app-id>",
		Short: "Delete an application",
		Args:  cobra.ExactArgs(1),
		RunE:  runBunnyAppDelete,
	}
}

func runBunnyAppDelete(cmd *cobra.Command, args []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	return c.DeleteApp(ctx, args[0])
}

func newBunnyAppDeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deploy <app-id>",
		Short: "Deploy (start) an application",
		Args:  cobra.ExactArgs(1),
		RunE:  runBunnyAppDeploy,
	}
}

func runBunnyAppDeploy(cmd *cobra.Command, args []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()
	return c.DeployApp(ctx, args[0])
}

func newBunnyAppRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <app-id>",
		Short: "Restart all pods of an application",
		Args:  cobra.ExactArgs(1),
		RunE:  runBunnyAppRestart,
	}
}

func runBunnyAppRestart(cmd *cobra.Command, args []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()
	return c.RestartApp(ctx, args[0])
}

// ============================================================================
// bunny dns subcommands
// ============================================================================

func newBunnyDNSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns",
		Short: "Manage DNS zones and records",
	}

	cmd.AddCommand(newBunnyDNSZoneListCmd())
	cmd.AddCommand(newBunnyDNSZoneAddCmd())
	cmd.AddCommand(newBunnyDNSZoneDeleteCmd())
	cmd.AddCommand(newBunnyDNSRecordListCmd())
	cmd.AddCommand(newBunnyDNSRecordAddCmd())
	cmd.AddCommand(newBunnyDNSRecordDeleteCmd())

	return cmd
}

func newBunnyDNSZoneListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "zone-list",
		Short: "List all DNS zones",
		Args:  cobra.NoArgs,
		RunE:  runBunnyDNSZoneList,
	}
}

func runBunnyDNSZoneList(cmd *cobra.Command, _ []string) error {
	c := bunnyClient()
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	zones, err := c.ListDNSZones(ctx)
	if err != nil {
		return err
	}
	if len(zones) == 0 {
		fmt.Println("No DNS zones found.")
		return nil
	}
	for _, z := range zones {
		fmt.Printf("%d  %s\n", z.ID, z.Domain)
	}
	return nil
}

func newBunnyDNSZoneAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "zone-add <domain>",
		Short: "Add a DNS zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			return c.AddDNSZone(ctx, args[0])
		},
	}
}

func newBunnyDNSZoneDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "zone-delete <zone-id>",
		Short: "Delete a DNS zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			return c.DeleteDNSZone(ctx, id)
		},
	}
}

func newBunnyDNSRecordListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "record-list <zone-id>",
		Short: "List DNS records in a zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			records, err := c.ListDNSRecords(ctx, id)
			if err != nil {
				return err
			}
			if len(records) == 0 {
				fmt.Println("No DNS records found.")
				return nil
			}
			for _, r := range records {
				nm := r.Name
				if nm == "" {
					nm = "@"
				}
				fmt.Printf("%d  %-6s  %-30s  %s (TTL:%d)\n",
					r.ID, bunny.DNSRecordTypeName(r.Type), nm, r.Value, r.TTL)
			}
			return nil
		},
	}
}

func newBunnyDNSRecordAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record-add <zone-id>",
		Short: "Add a DNS record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			zoneID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			if bf.ttl == 0 {
				bf.ttl = 120
			}
			if bf.origin == "" {
				return fmt.Errorf("--value is required")
			}

			recordTypeInt := bunny.ParseDNSRecordType(bf.recordType)

			record := bunny.AddDNSRecordModel{
				Type:  recordTypeInt,
				Name:  bf.hostname,
				Value: bf.origin,
				TTL:   bf.ttl,
			}
			if bf.priority > 0 {
				p := bf.priority
				record.Priority = &p
			}

			resp, err := c.AddDNSRecord(ctx, zoneID, record)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Record added: ID %d\n", resp.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&bf.recordType, "type", "t", "A", "DNS record type (A, AAAA, CNAME, TXT, MX, SRV, CAA, NS, PTR, HTTPS, SVCB, TLSA)")
	cmd.Flags().StringVarP(&bf.hostname, "name", "n", "@", "Record name (e.g. www, @ for root)")
	cmd.Flags().StringVarP(&bf.origin, "value", "v", "", "Record value")
	cmd.Flags().Int32Var(&bf.ttl, "ttl", 120, "TTL in seconds")
	cmd.Flags().Int32VarP(&bf.priority, "priority", "p", 0, "Priority (for MX records)")
	return cmd
}

func newBunnyDNSRecordDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "record-delete <zone-id> <record-id>",
		Short: "Delete a DNS record",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			zoneID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			recordID, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid record ID: %w", err)
			}
			return c.DeleteDNSRecord(ctx, zoneID, recordID)
		},
	}
}

// ============================================================================
// bunny pullzone subcommands
// ============================================================================

func newBunnyPullZoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pullzone",
		Short: "Manage CDN Pull Zones",
	}

	cmd.AddCommand(newBunnyPullZoneListCmd())
	cmd.AddCommand(newBunnyPullZoneCreateCmd())
	cmd.AddCommand(newBunnyPullZoneDeleteCmd())
	cmd.AddCommand(newBunnyPullZonePurgeCmd())
	cmd.AddCommand(newBunnyPullZoneEdgeRuleCmd())

	return cmd
}

func newBunnyPullZoneEdgeRuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edge-rule",
		Short: "Manage Pull Zone Edge Rules",
	}

	add := &cobra.Command{
		Use:   "add <zone-id>",
		Short: "Add an Edge Rule to a Pull Zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zoneID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			rule := bunny.EdgeRule{
				Description: bf.hostname,
				Enabled:     true,
				Triggers: []bunny.EdgeRuleTrigger{
					{Type: 0, Parameter1: "/*"},
				},
				Actions: []bunny.EdgeRuleAction{
					{Type: 3, Parameter1: bf.origin},
				},
			}
			return c.AddEdgeRule(ctx, zoneID, rule)
		},
	}
	add.Flags().StringVarP(&bf.hostname, "description", "d", "", "Rule description")
	add.Flags().StringVarP(&bf.origin, "set-header", "s", "", "Header to set (e.g. X-Custom: value)")

	list := &cobra.Command{
		Use:   "list <zone-id>",
		Short: "List Edge Rules for a Pull Zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zoneID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			zone, err := c.GetPullZone(ctx, zoneID)
			if err != nil {
				return err
			}
			for _, r := range zone.Hostnames {
				fmt.Printf("%s  (force SSL: %v)\n", r.Value, r.ForceSSL)
			}
			return nil
		},
	}

	del := &cobra.Command{
		Use:   "delete <zone-id> <rule-id>",
		Short: "Delete an Edge Rule",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zoneID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			return c.DeleteEdgeRule(ctx, zoneID, args[1])
		},
	}

	cmd.AddCommand(add)
	cmd.AddCommand(list)
	cmd.AddCommand(del)
	return cmd
}

func newBunnyPullZoneListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all Pull Zones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			zones, err := c.ListPullZones(ctx)
			if err != nil {
				return err
			}
			if len(zones) == 0 {
				fmt.Println("No Pull Zones found.")
				return nil
			}
			for _, z := range zones {
				hosts := ""
				if len(z.Hostnames) > 0 {
					hosts = z.Hostnames[0].Value
				}
				fmt.Printf("%d  %s  %s  origin:%s\n", z.ID, z.Name, hosts, z.OriginURL)
			}
			return nil
		},
	}
}

func newBunnyPullZoneCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a Pull Zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			model := bunny.PullZoneAddModel{
				Name:      args[0],
				OriginURL: bf.origin,
			}
			return c.CreatePullZone(ctx, model)
		},
	}
	cmd.Flags().StringVarP(&bf.origin, "origin", "o", "", "Origin URL (e.g. https://origin.example.com)")
	_ = cmd.MarkFlagRequired("origin")
	return cmd
}

func newBunnyPullZoneDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <zone-id>",
		Short: "Delete a Pull Zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			return c.DeletePullZone(ctx, id)
		},
	}
}

func newBunnyPullZonePurgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "purge <zone-id>",
		Short: "Purge Pull Zone cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid zone ID: %w", err)
			}
			return c.PurgePullZoneCache(ctx, id, "")
		},
	}
}

// ============================================================================
// bunny script subcommands (Edge Scripting)
// ============================================================================

func newBunnyScriptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "script",
		Short: "Manage Edge Scripting scripts",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create an Edge Script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			resp, err := c.CreateEdgeScript(ctx, bunny.AddEdgeScriptModel{Name: args[0]})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Script created: ID %d\n", resp.ID)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List Edge Scripts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			scripts, err := c.ListEdgeScripts(ctx)
			if err != nil {
				return err
			}
			if len(scripts) == 0 {
				fmt.Println("No Edge Scripts found.")
				return nil
			}
			for _, s := range scripts {
				fmt.Printf("%d  %s\n", s.ID, s.Name)
			}
			return nil
		},
	})

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an Edge Script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid script ID: %w", err)
			}
			return c.DeleteEdgeScript(ctx, id)
		},
	}

	setCodeCmd := &cobra.Command{
		Use:   "set-code <id> <file>",
		Short: "Set script code from a file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid script ID: %w", err)
			}
			code, err := os.ReadFile(args[1])
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			return c.SetEdgeScriptCode(ctx, id, string(code))
		},
	}

	publishCmd := &cobra.Command{
		Use:   "publish <id>",
		Short: "Publish (deploy) an Edge Script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid script ID: %w", err)
			}
			return c.PublishRelease(ctx, id)
		},
	}

	cmd.AddCommand(deleteCmd)
	cmd.AddCommand(setCodeCmd)
	cmd.AddCommand(publishCmd)

	return cmd
}

// ============================================================================
// bunny storage subcommands
// ============================================================================

func newBunnyStorageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Manage Edge Storage zones and files",
	}
	cmd.AddCommand(newBunnyStorageZoneCmd())
	cmd.AddCommand(newBunnyStorageFileCmd())
	return cmd
}

func newBunnyStorageZoneCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "zone", Short: "Manage storage zones"}

	list := &cobra.Command{
		Use: "list", Short: "List storage zones", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zones, err := c.ListStorageZones(ctx)
			if err != nil {
				return err
			}
			if len(zones) == 0 {
				fmt.Println("No storage zones found.")
				return nil
			}
			for _, z := range zones {
				fmt.Printf("%d  %s  region=%s  storage=%.1fGB\n", z.ID, z.Name, z.Region, float64(z.StorageUsed)/1e9)
			}
			return nil
		},
	}

	create := &cobra.Command{
		Use: "create <name>", Short: "Create a storage zone", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zone, err := c.CreateStorageZone(ctx, bunny.AddStorageZoneModel{Name: args[0], Region: bf.origin})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Zone created: ID %d\n", zone.ID)
			fmt.Printf("  Hostname: %s\n", zone.StorageHostname)
			return nil
		},
	}
	create.Flags().StringVarP(&bf.origin, "region", "r", "DE", "Region (DE, NY, LA, SG, SYD)")

	del := &cobra.Command{
		Use: "delete <id>", Short: "Delete a storage zone", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id: %w", err)
			}
			return c.DeleteStorageZone(ctx, id)
		},
	}

	cmd.AddCommand(list)
	cmd.AddCommand(create)
	cmd.AddCommand(del)
	return cmd
}

func getStorageAccessKey(ctx context.Context, c *bunny.Client, zoneName string) (string, error) {
	zones, err := c.ListStorageZones(ctx)
	if err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.Name == zoneName {
			if z.Password != "" {
				return z.Password, nil
			}
			return "", fmt.Errorf("zone %q has no password", zoneName)
		}
	}
	return "", fmt.Errorf("zone %q not found", zoneName)
}

func newBunnyStorageFileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "file", Short: "Manage files in a storage zone"}

	fileList := &cobra.Command{
		Use: "list <zone-name> <path>", Short: "List files", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			ak, err := getStorageAccessKey(ctx, c, args[0])
			if err != nil {
				return err
			}
			files, err := c.ListStorageFiles(ctx, args[0], args[1], ak)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Println("No files found.")
				return nil
			}
			for _, f := range files {
				dir := " "
				if f.IsDirectory {
					dir = "D"
				}
				fmt.Printf("%s %-50s %8d bytes  %s\n", dir, f.ObjectName, f.Size, f.LastChanged)
			}
			return nil
		},
	}

	fileUpload := &cobra.Command{
		Use: "upload <zone-name> <path> <file>", Short: "Upload a file", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			ak, err := getStorageAccessKey(ctx, c, args[0])
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[2])
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			parts := strings.Split(args[2], "/")
			fileName := parts[len(parts)-1]
			return c.UploadStorageFile(ctx, args[0], args[1], fileName, data, ak)
		},
	}

	fileDownload := &cobra.Command{
		Use: "download <zone-name> <path> <output>", Short: "Download a file", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			ak, err := getStorageAccessKey(ctx, c, args[0])
			if err != nil {
				return err
			}
			data, err := c.DownloadStorageFile(ctx, args[0], args[1], ak)
			if err != nil {
				return err
			}
			if err := os.WriteFile(args[2], data, 0600); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
			fmt.Printf("✓ Downloaded %d bytes to %s\n", len(data), args[2])
			return nil
		},
	}

	fileDelete := &cobra.Command{
		Use: "delete <zone-name> <path>", Short: "Delete a file", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			ak, err := getStorageAccessKey(ctx, c, args[0])
			if err != nil {
				return err
			}
			return c.DeleteStorageFile(ctx, args[0], args[1], ak)
		},
	}

	cmd.AddCommand(fileList)
	cmd.AddCommand(fileUpload)
	cmd.AddCommand(fileDownload)
	cmd.AddCommand(fileDelete)
	return cmd
}

// ============================================================================
// bunny stream subcommands
// ============================================================================

func newBunnyStreamCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stream", Short: "Manage Stream video libraries and videos"}
	cmd.AddCommand(newBunnyStreamLibCmd())
	cmd.AddCommand(newBunnyStreamVideoCmd())
	return cmd
}

func newBunnyStreamLibCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "library", Short: "Manage video libraries"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List video libraries", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			libs, err := c.ListVideoLibraries(ctx)
			if err != nil { return err }
			if len(libs) == 0 { fmt.Println("No video libraries found."); return nil }
			for _, l := range libs { fmt.Printf("%d  %s  videos=%d  storage=%dMB\n", l.ID, l.Name, l.VideoCount, l.TotalStorage/1e6) }
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{Use: "create <name>", Short: "Create a video library", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			lib, err := c.CreateVideoLibrary(ctx, args[0])
			if err != nil { return err }
			fmt.Printf("✓ Library created: ID %d\n", lib.ID)
			return nil
		},
	})
	return cmd
}

func newBunnyStreamVideoCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "video", Short: "Manage videos"}
	cmd.AddCommand(&cobra.Command{Use: "create <id> <title>", Short: "Create a video", Args: cobra.ExactArgs(2), RunE: runStreamCreate})
	cmd.AddCommand(&cobra.Command{Use: "list <id>", Short: "List videos", Args: cobra.ExactArgs(1), RunE: runStreamList})
	cmd.AddCommand(&cobra.Command{Use: "get <id> <vid>", Short: "Get video details", Args: cobra.ExactArgs(2), RunE: runStreamGet})
	cmd.AddCommand(&cobra.Command{Use: "fetch <id> <url>", Short: "Import video from URL", Args: cobra.ExactArgs(2), RunE: runStreamFetch})
	cmd.AddCommand(&cobra.Command{Use: "delete <id> <vid>", Short: "Delete a video", Args: cobra.ExactArgs(2), RunE: runStreamDelete})
	return cmd
}

func runStreamCreate(cmd *cobra.Command, args []string) error {
	c := bunnyClient(); ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	libID := parseInt64(args[0]); ak, err := c.GetLibraryAPIKey(ctx, libID)
	if err != nil { return err }
	v, err := c.CreateVideo(ctx, libID, ak, args[1])
	if err != nil { return err }
	fmt.Printf("✓ Video created: GUID %s\nUpload URL: %s\n", v.GUID, v.UploadURL)
	return nil
}

func runStreamList(cmd *cobra.Command, args []string) error {
	c := bunnyClient(); ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	libID := parseInt64(args[0]); ak, err := c.GetLibraryAPIKey(ctx, libID)
	if err != nil { return err }
	videos, err := c.ListVideos(ctx, libID, ak, 1, 100)
	if err != nil { return err }
	if len(videos) == 0 { fmt.Println("No videos found."); return nil }
	for _, v := range videos {
		s := "queued"; switch v.Status { case 1: s = "processing"; case 2: s = "ready"; case 3: s = "failed" }
		fmt.Printf("%s  %s  [%s]  %ds  %dx%d\n", v.ID, v.Title, s, v.Length, v.Width, v.Height)
	}
	return nil
}

func runStreamGet(cmd *cobra.Command, args []string) error {
	c := bunnyClient(); ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	libID := parseInt64(args[0]); ak, err := c.GetLibraryAPIKey(ctx, libID)
	if err != nil { return err }
	v, err := c.GetVideo(ctx, libID, ak, args[1])
	if err != nil { return err }
	s := "queued"; switch v.Status { case 1: s = "processing"; case 2: s = "ready"; case 3: s = "failed" }
	fmt.Printf("ID: %s\nTitle: %s\nStatus: %s\nCreated: %s\nLength: %ds\nSize: %dMB\nViews: %d\n", v.ID, v.Title, s, v.DateCreated, v.Length, v.StorageSize/1e6, v.Views)
	return nil
}

func runStreamFetch(cmd *cobra.Command, args []string) error {
	c := bunnyClient(); ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	libID := parseInt64(args[0]); ak, err := c.GetLibraryAPIKey(ctx, libID)
	if err != nil { return err }
	v, err := c.FetchVideo(ctx, libID, ak, args[1])
	if err != nil { return err }
	fmt.Printf("✓ Video fetching: %s\n", v.ID)
	return nil
}

func runStreamDelete(cmd *cobra.Command, args []string) error {
	c := bunnyClient(); ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	libID := parseInt64(args[0]); ak, err := c.GetLibraryAPIKey(ctx, libID)
	if err != nil { return err }
	return c.DeleteVideo(ctx, libID, ak, args[1])
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// ============================================================================
// bunny shield subcommands
// ============================================================================

func newBunnyShieldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shield",
		Short: "Manage Shield WAF security zones",
	}

	cmd.AddCommand(&cobra.Command{
		Use: "zone-list", Short: "List Shield zones", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zones, err := c.ListShieldZones(ctx)
			if err != nil {
				return err
			}
			if len(zones) == 0 {
				fmt.Println("No Shield zones found.")
				return nil
			}
			for _, z := range zones {
				fmt.Printf("%d  PullZone=%d  %s\n", z.ID, z.PullZoneID, z.Name)
			}
			return nil
		},
	})

	rateCmd := &cobra.Command{
		Use: "rate-limit", Short: "Manage rate limits",
	}

	rateCmd.AddCommand(&cobra.Command{
		Use: "list <zone-id>", Short: "List rate limits", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := bunnyClient()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			zoneID := parseInt64(args[0])
			rules, err := c.ListRateLimits(ctx, zoneID)
			if err != nil {
				return err
			}
			for _, r := range rules {
				fmt.Printf("ID=%d  limit=%d  window=%ds  action=%s  path=%s\n", r.ID, r.RequestsLimit, r.WindowLength, r.Action, r.Path)
			}
			return nil
		},
	})

	cmd.AddCommand(rateCmd)
	return cmd
}
