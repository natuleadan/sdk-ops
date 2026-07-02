package terraform

import (
	"fmt"
	"strings"

	"github.com/natuleadan/sdk-ops/providers"
)

func ExportVPS(providerName string, vps providers.VPS) string {
	var b strings.Builder

	fmt.Fprintf(&b, `resource "%s_vps" "%s" {
`, providerName, sanitizeName(vps.Name))

	if vps.Plan != "" {
		fmt.Fprintf(&b, "  plan     = %q\n", vps.Plan)
	}
	if vps.Location != "" {
		fmt.Fprintf(&b, "  location = %q\n", vps.Location)
	}
	if vps.Template != "" {
		fmt.Fprintf(&b, "  template = %q\n", vps.Template)
	}
	if vps.Label != "" {
		fmt.Fprintf(&b, "  label    = %q\n", vps.Label)
	}

	fmt.Fprintf(&b, "  # Import: terraform import %s_vps.%s %s\n", providerName, sanitizeName(vps.Name), vps.ID)
	b.WriteString("}\n")

	return b.String()
}

func ExportK8s(providerName string, cluster providers.K8sCluster) string {
	var b strings.Builder

	fmt.Fprintf(&b, `resource "%s_kubernetes" "%s" {
`, providerName, sanitizeName(cluster.Name))

	if cluster.Version != "" {
		fmt.Fprintf(&b, "  version    = %q\n", cluster.Version)
	}
	if cluster.Location != "" {
		fmt.Fprintf(&b, "  location   = %q\n", cluster.Location)
	}
	if cluster.NodeCount > 0 {
		fmt.Fprintf(&b, "  node_count = %d\n", cluster.NodeCount)
	}

	fmt.Fprintf(&b, "  # Import: terraform import %s_kubernetes.%s %s\n", providerName, sanitizeName(cluster.Name), cluster.ID)
	b.WriteString("}\n")

	return b.String()
}

func ExportLB(providerName string, lb providers.LoadBalancer) string {
	var b strings.Builder
	fmt.Fprintf(&b, `resource "%s_load_balancer" "%s" {
`, providerName, sanitizeName(lb.Name))
	fmt.Fprintf(&b, "  # Import: terraform import %s_load_balancer.%s %s\n", providerName, sanitizeName(lb.Name), lb.ID)
	b.WriteString("}\n")
	return b.String()
}

func sanitizeName(name string) string {
	r := strings.NewReplacer(
		".", "-",
		"_", "-",
		" ", "-",
	)
	return strings.ToLower(r.Replace(name))
}
