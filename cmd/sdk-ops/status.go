package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/monitor"
	"github.com/natuleadan/sdk-ops/ssh"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show unified health dashboard for all nodes",
		Long: `Show a unified dashboard of all registered nodes with:
  system health (CPU, RAM, disk, kernel, uptime)
  runtime status (Docker, k3s)
  deployed services
  agent health

Examples:
  sdk-ops status
  sdk-ops status --node 188.xxx.xxx.xxx`,
		RunE: statusRunE,
	}
	cmd.Flags().StringP("node", "n", "", "Node IP (default: all registered nodes)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

type nodeWork struct {
	cfg  NodeConfig
	user string
	key  string
	port int
}

type statusResult struct {
	ip          string
	hostname    string
	stats       *monitor.NodeStats
	rt          *monitor.RuntimeStatus
	services    []string
	svcStatuses []string
	agent       string
	err         error
}

func statusRunE(c *cobra.Command, args []string) error {
	nodeIP, _ := c.Flags().GetString("node")
	user, _ := c.Flags().GetString("user")
	key, _ := c.Flags().GetString("key")
	port, _ := c.Flags().GetInt("port")

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	nodes := collectStatusNodes(cfg, nodeIP, user, key, port)
	if len(nodes) == 0 {
		fmt.Println("  No nodes registered. Use:")
		fmt.Println("    sdk-ops config add-node <ip> --user root --key ~/.ssh/id_ed25519")
		return nil
	}

	results := statusFetchAll(nodes)
	statusRenderAll(c, nodes, results)
	return nil
}

func collectStatusNodes(cfg *RootConfig, nodeIP, user, key string, port int) []nodeWork {
	if nodeIP != "" {
		u, k, p := user, key, port
		if n := lookupNode(nodeIP); n != nil {
			if u == "" {
				u = n.User
			}
			if k == "" {
				k = n.Key
			}
			if p == 0 {
				p = n.Port
			}
		}
		if u == "" {
			u = "root"
		}
		if p == 0 {
			p = 22
		}
		return []nodeWork{{NodeConfig{IP: nodeIP}, u, k, p}}
	}

	var nodes []nodeWork
	for _, n := range cfg.Nodes {
		u, k, p := n.User, n.Key, n.Port
		if u == "" {
			u = "root"
		}
		if p == 0 {
			p = 22
		}
		nodes = append(nodes, nodeWork{n, u, k, p})
	}
	return nodes
}

func statusFetchAll(nodes []nodeWork) []statusResult {
	results := make(chan statusResult, len(nodes))
	var wg sync.WaitGroup

	for _, n := range nodes {
		wg.Add(1)
		go func(nw nodeWork) {
			defer wg.Done()
			results <- statusFetchOne(nw)
		}(n)
	}
	wg.Wait()
	close(results)

	var out []statusResult
	for r := range results {
		out = append(out, r)
	}
	return out
}

func statusFetchOne(nw nodeWork) statusResult {
	r := statusResult{ip: nw.cfg.IP}

	client := newSSHClient(nw.cfg.IP, nw.user, nw.port, nw.key)
	conn, err := client.Connect()
	if err != nil {
		r.err = fmt.Errorf("ssh: %w", err)
		return r
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "status: conn close error: %v\n", err) } }()

	r.hostname, _, err = ssh.Run(conn, "hostname 2>/dev/null | tr -d '\n'")
	if err != nil {
		log.Printf("status: hostname error: %v", err)
	}
	r.hostname = strings.TrimSpace(r.hostname)

	stats, err := monitor.GetStats(conn)
	if err != nil {
		r.err = fmt.Errorf("get stats: %w", err)
		return r
	}
	r.stats = stats
	r.rt = monitor.GetRuntimeStatus(conn)

	services, _ := deploy.ListServices(conn)
	r.services = services
	for _, svc := range services {
		s, _ := deploy.ServiceStatus(conn, svc)
		status := "?"
		if strings.Contains(s, "running") || strings.Contains(s, "type:") {
			status = "ok"
		}
		r.svcStatuses = append(r.svcStatuses, status)
	}

	agentOut, _, _ := ssh.Run(conn, `curl -s --max-time 3 http://localhost:9000/health 2>/dev/null || echo 'unreachable'`)
	agentOut = strings.TrimSpace(agentOut)
	switch {
	case strings.Contains(agentOut, `"status":"ok"`):
		r.agent = "healthy"
	case agentOut == "unreachable":
		r.agent = "offline"
	default:
		r.agent = "unknown"
	}

	return r
}

func statusRenderAll(c *cobra.Command, nodes []nodeWork, results []statusResult) {
	if _, err := fmt.Fprintf(c.OutOrStdout(), "\n  %ssdk-ops status — %d node(s)%s\n\n", colorBold, len(nodes), colorReset); err != nil { log.Printf("status: write error: %v", err) }

	totalServices := 0
	healthyNodes := 0
	warnNodes := 0
	criticalNodes := 0

	for _, r := range results {
		totalServices += len(r.services)

		if _, err := fmt.Fprintf(c.OutOrStdout(), "  %s━━━ %s ━━━%s\n", colorCyan, r.ip, colorReset); err != nil { log.Printf("status: write error: %v", err) }
		if r.err != nil {
			criticalNodes++
			if _, fErr := fmt.Fprintf(c.OutOrStdout(), "  %s✗ %s%s\n\n", colorRed, r.err, colorReset); fErr != nil { log.Printf("status: write error: %v", fErr) }
			continue
		}

		statusRenderNodeHeader(c, r)
		statusRenderNodeStats(c, r)
		statusRenderRuntime(c, r)
		statusRenderServices(c, r)

		crit, warn := statusClassifyNode(r)
		switch {
		case crit:
			criticalNodes++
		case warn:
			warnNodes++
		default:
			healthyNodes++
		}
		if _, err := fmt.Fprintln(c.OutOrStdout()); err != nil { log.Printf("status: write error: %v", err) }
	}

	statusRenderSummary(c, nodes, totalServices, healthyNodes, warnNodes, criticalNodes)
}

func statusRenderNodeHeader(c *cobra.Command, r statusResult) {
	hostLabel := r.hostname
	if hostLabel == "" {
		hostLabel = r.ip
	}
	runtimeLabel := ""
	if r.rt != nil {
		switch {
		case r.rt.DockerOK == "yes" && r.rt.K3sRunning == "yes":
			runtimeLabel = "docker+k3s"
		case r.rt.K3sRunning == "yes":
			runtimeLabel = "k3s"
		case r.rt.DockerOK == "yes":
			runtimeLabel = "docker"
		}
	}
	if runtimeLabel != "" {
		hostLabel = fmt.Sprintf("%s [%s]", hostLabel, runtimeLabel)
	}

	switch r.agent {
	case "healthy":
		hostLabel += fmt.Sprintf("  %sagent ✓%s", colorGreen, colorReset)
	case "offline":
		hostLabel += fmt.Sprintf("  %sagent ✗%s", colorRed, colorReset)
	default:
		hostLabel += fmt.Sprintf("  %sagent ?%s", colorYellow, colorReset)
	}
	if _, err := fmt.Fprintf(c.OutOrStdout(), "  %s%s%s\n", colorBold, hostLabel, colorReset); err != nil { log.Printf("status: write error: %v", err) }
}

func statusRenderNodeStats(c *cobra.Command, r statusResult) {
	s := r.stats
	if s == nil {
		return
	}
	cpuPct := parsePercent(s.CPU)
	memPct := parsePercent(s.Memory)
	diskPct := parsePercentStr(s.Disk)

	if _, err := fmt.Fprintf(c.OutOrStdout(), "  CPU:    %s%.0f%%%s  load: %s\n", colorForPct(cpuPct), cpuPct, colorReset, s.CPULoad); err != nil { log.Printf("status: write error: %v", err) }
	if s.MemUsed != "" && s.MemTotal != "" {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Mem:    %s%.0f%%%s  (%s / %s)\n", colorForPct(memPct), memPct, colorReset, s.MemUsed, s.MemTotal); err != nil { log.Printf("status: write error: %v", err) }
	}
	if s.DiskUsed != "" && s.DiskSize != "" {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Disk:   %s%.0f%%%s  (%s / %s)\n", colorForPct(diskPct), diskPct, colorReset, s.DiskUsed, s.DiskSize); err != nil { log.Printf("status: write error: %v", err) }
	}
	if s.Kernel != "" {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Kernel: %s\n", s.Kernel); err != nil { log.Printf("status: write error: %v", err) }
	}
	if s.Uptime != "" {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Uptime: %s\n", s.Uptime); err != nil { log.Printf("status: write error: %v", err) }
	}
}

func statusRenderRuntime(c *cobra.Command, r statusResult) {
	if r.rt == nil {
		return
	}
	if r.rt.DockerVer != "" {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Docker: %s  (running: %s)\n", r.rt.DockerVer, r.rt.DockerOK); err != nil { log.Printf("status: write error: %v", err) }
	}
	if r.rt.K3sVersion != "" {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  k3s:    %s  (running: %s)\n", r.rt.K3sVersion, r.rt.K3sRunning); err != nil { log.Printf("status: write error: %v", err) }
	}
}

func statusRenderServices(c *cobra.Command, r statusResult) {
	if len(r.services) > 0 {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Services (%d):\n", len(r.services)); err != nil { log.Printf("status: write error: %v", err) }
		for i, svc := range r.services {
			icon := "?"
			svcColor := colorYellow
			if i < len(r.svcStatuses) {
				if r.svcStatuses[i] == "ok" {
					icon = "✓"
					svcColor = colorGreen
				} else {
					icon = "✗"
					svcColor = colorRed
				}
			}
			if _, err := fmt.Fprintf(c.OutOrStdout(), "    %s%s %s%s\n", svcColor, icon, svc, colorReset); err != nil { log.Printf("status: write error: %v", err) }
		}
	} else {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  %sServices: none%s\n", colorYellow, colorReset); err != nil { log.Printf("status: write error: %v", err) }
	}
}

func statusClassifyNode(r statusResult) (crit, warn bool) {
	s := r.stats
	if s != nil {
		if parsePercent(s.CPU) >= 90 || parsePercent(s.Memory) >= 90 || parsePercentStr(s.Disk) >= 90 {
			crit = true
		} else if parsePercent(s.CPU) >= 70 || parsePercent(s.Memory) >= 70 || parsePercentStr(s.Disk) >= 70 {
			warn = true
		}
	}
	if r.agent == "offline" {
		crit = true
	}
	return
}

func statusRenderSummary(c *cobra.Command, nodes []nodeWork, totalServices, healthyNodes, warnNodes, criticalNodes int) {
	if _, err := fmt.Fprintf(c.OutOrStdout(), "  %s━━━ Summary ━━━%s\n", colorBold, colorReset); err != nil { log.Printf("status: write error: %v", err) }
	if _, err := fmt.Fprintf(c.OutOrStdout(), "  Nodes:    %d total\n", len(nodes)); err != nil { log.Printf("status: write error: %v", err) }
	if healthyNodes > 0 {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  %s  %d healthy%s\n", colorGreen, healthyNodes, colorReset); err != nil { log.Printf("status: write error: %v", err) }
	}
	if warnNodes > 0 {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  %s  %d warning%s\n", colorYellow, warnNodes, colorReset); err != nil { log.Printf("status: write error: %v", err) }
	}
	if criticalNodes > 0 {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  %s  %d critical%s\n", colorRed, criticalNodes, colorReset); err != nil { log.Printf("status: write error: %v", err) }
	}
	if totalServices > 0 {
		if _, err := fmt.Fprintf(c.OutOrStdout(), "  Services: %d total\n", totalServices); err != nil { log.Printf("status: write error: %v", err) }
	}
	if _, err := fmt.Fprintln(c.OutOrStdout()); err != nil { log.Printf("status: write error: %v", err) }

	if criticalNodes > 0 {
		os.Exit(1)
	}
}

func parsePercent(s string) float64 {
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parsePercentStr(s string) float64 {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return 0
	}
	return parsePercent(parts[0])
}

func colorForPct(pct float64) string {
	if pct >= 90 {
		return colorRed
	} else if pct >= 70 {
		return colorYellow
	}
	return colorGreen
}
