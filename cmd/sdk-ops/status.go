package main

import (
	"fmt"
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
		RunE: func(c *cobra.Command, args []string) error {
			nodeIP, _ := c.Flags().GetString("node")
			user, _ := c.Flags().GetString("user")
			key, _ := c.Flags().GetString("key")
			port, _ := c.Flags().GetInt("port")

			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			type nodeWork struct {
				cfg  NodeConfig
				user string
				key  string
				port int
			}

			var nodes []nodeWork
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
				nodes = append(nodes, nodeWork{NodeConfig{IP: nodeIP}, u, k, p})
			} else {
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
			}

			if len(nodes) == 0 {
				fmt.Println("  No nodes registered. Use:")
				fmt.Println("    sdk-ops config add-node <ip> --user root --key ~/.ssh/id_ed25519")
				return nil
			}

			type result struct {
				ip          string
				hostname    string
				stats       *monitor.NodeStats
				rt          *monitor.RuntimeStatus
				services    []string
				svcStatuses []string
				agent       string
				err         error
			}

			results := make(chan result, len(nodes))
			var wg sync.WaitGroup

			for _, n := range nodes {
				wg.Add(1)
				go func(nw nodeWork) {
					defer wg.Done()
					r := result{ip: nw.cfg.IP}
					defer func() { results <- r }()

					client := newSSHClient(nw.cfg.IP, nw.user, nw.port, nw.key)
					conn, err := client.Connect()
					if err != nil {
						r.err = fmt.Errorf("ssh: %w", err)
						return
					}
					defer conn.Close()

					r.hostname, _, _ = ssh.Run(conn, "hostname 2>/dev/null | tr -d '\n'")
					r.hostname = strings.TrimSpace(r.hostname)

					stats, err := monitor.GetStats(conn)
					if err != nil {
						r.err = fmt.Errorf("get stats: %w", err)
						return
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
					if strings.Contains(agentOut, `"status":"ok"`) {
						r.agent = "healthy"
					} else if agentOut == "unreachable" {
						r.agent = "offline"
					} else {
						r.agent = "unknown"
					}
				}(n)
			}
			wg.Wait()
			close(results)

			fmt.Fprintf(c.OutOrStdout(), "\n  %ssdk-ops status — %d node(s)%s\n\n", colorBold, len(nodes), colorReset)

			totalServices := 0
			healthyNodes := 0
			warnNodes := 0
			criticalNodes := 0

			for r := range results {
				totalServices += len(r.services)

				fmt.Fprintf(c.OutOrStdout(), "  %s━━━ %s ━━━%s\n", colorCyan, r.ip, colorReset)
				if r.err != nil {
					criticalNodes++
					fmt.Fprintf(c.OutOrStdout(), "  %s✗ %s%s\n\n", colorRed, r.err, colorReset)
					continue
				}

				// Header line
				hostLabel := r.hostname
				if hostLabel == "" {
					hostLabel = r.ip
				}
				runtimeLabel := ""
				if r.rt != nil {
					if r.rt.DockerOK == "yes" && r.rt.K3sRunning == "yes" {
						runtimeLabel = "docker+k3s"
					} else if r.rt.K3sRunning == "yes" {
						runtimeLabel = "k3s"
					} else if r.rt.DockerOK == "yes" {
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
				fmt.Fprintf(c.OutOrStdout(), "  %s%s%s\n", colorBold, hostLabel, colorReset)

				// System stats
				s := r.stats
				if s != nil {
					cpuPct := parsePercent(s.CPU)
					memPct := parsePercent(s.Memory)
					diskPct := parsePercentStr(s.Disk)

					fmt.Fprintf(c.OutOrStdout(), "  CPU:    %s%.0f%%%s  load: %s\n", colorForPct(cpuPct), cpuPct, colorReset, s.CPULoad)
					if s.MemUsed != "" && s.MemTotal != "" {
						fmt.Fprintf(c.OutOrStdout(), "  Mem:    %s%.0f%%%s  (%s / %s)\n", colorForPct(memPct), memPct, colorReset, s.MemUsed, s.MemTotal)
					}
					if s.DiskUsed != "" && s.DiskSize != "" {
						fmt.Fprintf(c.OutOrStdout(), "  Disk:   %s%.0f%%%s  (%s / %s)\n", colorForPct(diskPct), diskPct, colorReset, s.DiskUsed, s.DiskSize)
					}
					if s.Kernel != "" {
						fmt.Fprintf(c.OutOrStdout(), "  Kernel: %s\n", s.Kernel)
					}
					if s.Uptime != "" {
						fmt.Fprintf(c.OutOrStdout(), "  Uptime: %s\n", s.Uptime)
					}
				}

				// Runtime details
				if r.rt != nil {
					if r.rt.DockerVer != "" {
						fmt.Fprintf(c.OutOrStdout(), "  Docker: %s  (running: %s)\n", r.rt.DockerVer, r.rt.DockerOK)
					}
					if r.rt.K3sVersion != "" {
						fmt.Fprintf(c.OutOrStdout(), "  k3s:    %s  (running: %s)\n", r.rt.K3sVersion, r.rt.K3sRunning)
					}
				}

				// Services
				if len(r.services) > 0 {
					fmt.Fprintf(c.OutOrStdout(), "  Services (%d):\n", len(r.services))
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
						fmt.Fprintf(c.OutOrStdout(), "    %s%s %s%s\n", svcColor, icon, svc, colorReset)
					}
				} else {
					fmt.Fprintf(c.OutOrStdout(), "  %sServices: none%s\n", colorYellow, colorReset)
				}

				// Health classification
				warn := false
				crit := false
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
				switch {
				case crit:
					criticalNodes++
				case warn:
					warnNodes++
				default:
					healthyNodes++
				}
				fmt.Fprintln(c.OutOrStdout())
			}

			// Summary bar
			fmt.Fprintf(c.OutOrStdout(), "  %s━━━ Summary ━━━%s\n", colorBold, colorReset)
			fmt.Fprintf(c.OutOrStdout(), "  Nodes:    %d total\n", len(nodes))
			if healthyNodes > 0 {
				fmt.Fprintf(c.OutOrStdout(), "  %s  %d healthy%s\n", colorGreen, healthyNodes, colorReset)
			}
			if warnNodes > 0 {
				fmt.Fprintf(c.OutOrStdout(), "  %s  %d warning%s\n", colorYellow, warnNodes, colorReset)
			}
			if criticalNodes > 0 {
				fmt.Fprintf(c.OutOrStdout(), "  %s  %d critical%s\n", colorRed, criticalNodes, colorReset)
			}
			if totalServices > 0 {
				fmt.Fprintf(c.OutOrStdout(), "  Services: %d total\n", totalServices)
			}
			fmt.Fprintln(c.OutOrStdout())

			if criticalNodes > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringP("node", "n", "", "Node IP (default: all registered nodes)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
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
