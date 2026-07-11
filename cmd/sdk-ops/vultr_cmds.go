package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/providers/vultr"
)

func newProviderFirewallCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "firewall", Short: "Manage firewall groups and rules (Vultr only)"}
	cmd.AddCommand(newProviderFirewallGroupCmd())
	cmd.AddCommand(newProviderFirewallRuleCmd())
	return cmd
}

func newProviderFirewallGroupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "group", Short: "Manage firewall groups"}
	cmd.AddCommand(&cobra.Command{Use: "create <desc>", Short: "Create a firewall group", Args: cobra.ExactArgs(1), RunE: runFirewallGroupCreate})
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List firewall groups", Args: cobra.NoArgs, RunE: runFirewallGroupList})
	cmd.AddCommand(&cobra.Command{Use: "delete <id>", Short: "Delete a firewall group", Args: cobra.ExactArgs(1), RunE: runFirewallGroupDelete})
	return cmd
}

func newProviderFirewallRuleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rule", Short: "Manage firewall rules"}
	cmd.AddCommand(&cobra.Command{Use: "list <gid>", Short: "List firewall rules", Args: cobra.ExactArgs(1), RunE: runFirewallRuleList})
	cmd.AddCommand(&cobra.Command{Use: "add <gid>", Short: "Add firewall rule (SSH allow)", Args: cobra.ExactArgs(1), RunE: runFirewallRuleAdd})
	cmd.AddCommand(&cobra.Command{Use: "delete <gid> <rid>", Short: "Delete a firewall rule", Args: cobra.ExactArgs(2), RunE: runFirewallRuleDelete})
	return cmd
}

func getVultrClient() (*vultr.Client, error) {
	prov, err := getProvider()
	if err != nil {
		return nil, err
	}
	v, ok := prov.(*vultr.Client)
	if !ok {
		return nil, fmt.Errorf("requires --provider vultr")
	}
	return v, nil
}

func runFirewallGroupCreate(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	g, err := v.CreateFirewallGroup(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("✓ Group created: %s\n", g.ID)
	return nil
}

func runFirewallGroupList(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	groups, err := v.ListFirewallGroups(ctx)
	if err != nil {
		return err
	}
	for _, g := range groups {
		fmt.Printf("%s  %s  instances=%d  rules=%d\n", g.ID, g.Description, g.InstanceCount, g.RuleCount)
	}
	return nil
}

func runFirewallGroupDelete(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	return v.DeleteFirewallGroup(ctx, args[0])
}

func runFirewallRuleList(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	rules, err := v.ListFirewallRules(ctx, args[0])
	if err != nil {
		return err
	}
	for _, r := range rules {
		fmt.Printf("[%d] %s %s %s/%d port=%s action=%s\n", r.ID, r.IPType, r.Protocol, r.Subnet, r.SubnetSize, r.Port, r.Action)
	}
	return nil
}

func runFirewallRuleAdd(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	r, err := v.CreateFirewallRule(ctx, args[0], "v4", "tcp", "0.0.0.0", "22", "CLI", "", 0)
	if err != nil {
		return err
	}
	fmt.Printf("✓ Rule created: %d\n", r.ID)
	return nil
}

func runFirewallRuleDelete(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	ruleID, _ := strconv.Atoi(args[1])
	return v.DeleteFirewallRule(ctx, args[0], ruleID)
}

func newProviderObjectStorageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "object-storage", Short: "Manage S3 buckets (Vultr only)"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List S3 buckets", Args: cobra.NoArgs, RunE: runObjStorageList})
	cmd.AddCommand(&cobra.Command{Use: "clusters", Short: "List available S3 clusters", Args: cobra.NoArgs, RunE: runObjStorageClusters})
	cmd.AddCommand(&cobra.Command{Use: "create", Short: "Create S3 bucket (needs --node-count cluster_id)", Args: cobra.NoArgs, RunE: runObjStorageCreate})
	return cmd
}

func runObjStorageList(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	objs, err := v.ListObjectStorages(ctx)
	if err != nil {
		return err
	}
	for _, o := range objs {
		fmt.Printf("%s  %s  %s  s3=%s  status=%s\n", o.ID, o.Label, o.Region, o.S3Hostname, o.Status)
	}
	return nil
}

func runObjStorageClusters(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	clusters, err := v.ListObjectStorageClusters(ctx)
	if err != nil {
		return err
	}
	for _, c := range clusters {
		fmt.Printf("[%d] %s  %s\n", c.ID, c.Region, c.Hostname)
	}
	return nil
}

// --- CDN ---

func newProviderCDNCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cdn", Short: "Manage CDN pull zones (Vultr only)"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List CDN pull zones", Args: cobra.NoArgs, RunE: runCDNList})
	cmd.AddCommand(&cobra.Command{Use: "create <name> <origin>", Short: "Create a CDN pull zone", Args: cobra.ExactArgs(2), RunE: runCDNCreate})
	cmd.AddCommand(&cobra.Command{Use: "delete <id>", Short: "Delete a CDN pull zone", Args: cobra.ExactArgs(1), RunE: runCDNDelete})
	cmd.AddCommand(&cobra.Command{Use: "purge <id>", Short: "Purge CDN cache", Args: cobra.ExactArgs(1), RunE: runCDNPurge})
	return cmd
}

func runCDNList(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	zones, err := v.ListCDNPullZones(ctx)
	if err != nil { return err }
	for _, z := range zones {
		fmt.Printf("%s  %s  %s  %s\n", z.ID, z.Label, z.OriginDomain, z.Status)
	}
	return nil
}

func runCDNCreate(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	z, err := v.CreateCDNPullZone(ctx, args[0], args[1])
	if err != nil { return err }
	fmt.Printf("✓ CDN zone created: %s\n", z.ID)
	return nil
}

func runCDNDelete(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	return v.DeleteCDNPullZone(ctx, args[0])
}

func runCDNPurge(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	return v.PurgeCDNPullZone(ctx, args[0])
}

// --- Block Storage ---

func newProviderBlockStorageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "block-storage", Short: "Manage block storage volumes (Vultr only)"}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List block storage volumes", Args: cobra.NoArgs, RunE: runBlockList})
	cmd.AddCommand(&cobra.Command{Use: "create <label> <region> <size-gb>", Short: "Create a block storage volume", Args: cobra.ExactArgs(3), RunE: runBlockCreate})
	cmd.AddCommand(&cobra.Command{Use: "delete <id>", Short: "Delete a block storage volume", Args: cobra.ExactArgs(1), RunE: runBlockDelete})
	cmd.AddCommand(&cobra.Command{Use: "attach <id> <instance-id>", Short: "Attach a volume to an instance", Args: cobra.ExactArgs(2), RunE: runBlockAttach})
	cmd.AddCommand(&cobra.Command{Use: "detach <id>", Short: "Detach a volume", Args: cobra.ExactArgs(1), RunE: runBlockDetach})
	return cmd
}

func runBlockList(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	blocks, err := v.ListBlockStorages(ctx)
	if err != nil { return err }
	for _, b := range blocks {
		fmt.Printf("%s  %s  %dGB  %s  attached=%s\n", b.ID, b.Label, b.SizeGB, b.Region, b.AttachedTo)
	}
	return nil
}

func runBlockCreate(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	size, _ := strconv.Atoi(args[2])
	if size < 10 { size = 10 }
	b, err := v.CreateBlockStorage(ctx, args[0], args[1], size)
	if err != nil { return err }
	fmt.Printf("✓ Block storage created: %s\n", b.ID)
	return nil
}

func runBlockDelete(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	return v.DeleteBlockStorage(ctx, args[0])
}

func runBlockAttach(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	return v.AttachBlockStorage(ctx, args[0], args[1])
}

func runBlockDetach(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient(); if err != nil { return err }
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second); defer cancel()
	return v.DetachBlockStorage(ctx, args[0])
}

func runObjStorageCreate(cmd *cobra.Command, args []string) error {
	v, err := getVultrClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()
	obj, err := v.CreateObjectStorage(ctx, pf.nodeCount)
	if err != nil {
		return err
	}
	fmt.Printf("✓ Object storage created: %s\n", obj.ID)
	fmt.Printf("  S3 Hostname: %s\n", obj.S3Hostname)
	return nil
}
