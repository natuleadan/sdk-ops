package terraform

import (
	"fmt"
	"strings"

	"github.com/natuleadan/sdk-ops/providers"
)

func ExportVPS(providerName string, vps providers.VPS) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf(`resource "%s_vps" "%s" {
`, providerName, sanitizeName(vps.Name)))

	if vps.Plan != "" {
		b.WriteString(fmt.Sprintf("  plan     = %q\n", vps.Plan))
	}
	if vps.Location != "" {
		b.WriteString(fmt.Sprintf("  location = %q\n", vps.Location))
	}
	if vps.Template != "" {
		b.WriteString(fmt.Sprintf("  template = %q\n", vps.Template))
	}
	if vps.Label != "" {
		b.WriteString(fmt.Sprintf("  label    = %q\n", vps.Label))
	}

	b.WriteString(fmt.Sprintf("  # Import: terraform import %s_vps.%s %s\n", providerName, sanitizeName(vps.Name), vps.ID))
	b.WriteString("}\n")

	return b.String()
}

func ExportK8s(providerName string, cluster providers.K8sCluster) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf(`resource "%s_kubernetes" "%s" {
`, providerName, sanitizeName(cluster.Name)))

	if cluster.Version != "" {
		b.WriteString(fmt.Sprintf("  version    = %q\n", cluster.Version))
	}
	if cluster.Location != "" {
		b.WriteString(fmt.Sprintf("  location   = %q\n", cluster.Location))
	}
	if cluster.NodeCount > 0 {
		b.WriteString(fmt.Sprintf("  node_count = %d\n", cluster.NodeCount))
	}

	b.WriteString(fmt.Sprintf("  # Import: terraform import %s_kubernetes.%s %s\n", providerName, sanitizeName(cluster.Name), cluster.ID))
	b.WriteString("}\n")

	return b.String()
}

func ExportLB(providerName string, lb providers.LoadBalancer) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`resource "%s_load_balancer" "%s" {
`, providerName, sanitizeName(lb.Name)))
	b.WriteString(fmt.Sprintf("  # Import: terraform import %s_load_balancer.%s %s\n", providerName, sanitizeName(lb.Name), lb.ID))
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
