package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/ssh"
)

type Resource struct {
	ID        string            `yaml:"id"`
	Type      string            `yaml:"type"` // service, database, backup_schedule
	Name      string            `yaml:"name"`
	NodeIP    string            `yaml:"node_ip"`
	Version   string            `yaml:"version,omitempty"`
	Runtime   string            `yaml:"runtime,omitempty"`
	Status    string            `yaml:"status,omitempty"`
	CreatedAt string            `yaml:"created_at"`
	UpdatedAt string            `yaml:"updated_at"`
	Metadata  map[string]string `yaml:"metadata,omitempty"`
}

type State struct {
	Resources []Resource `yaml:"resources"`
}

func statePath() string {
	return filepath.Join(configDir(), "state.yaml")
}

func loadState() (*State, error) {
	var s State
	data, err := os.ReadFile(statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &s, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveState(s *State) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0600)
}

func stateRecord(resType, name, nodeIP, version, runtime, status string, meta map[string]string) {
	s, err := loadState()
	if err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := fmt.Sprintf("%s/%s/%s", nodeIP, resType, name)

	found := false
	for i, r := range s.Resources {
		if r.ID == id {
			s.Resources[i].Version = version
			s.Resources[i].Runtime = runtime
			s.Resources[i].Status = status
			s.Resources[i].UpdatedAt = now
			if meta != nil {
				s.Resources[i].Metadata = meta
			}
			found = true
			break
		}
	}
	if !found {
		s.Resources = append(s.Resources, Resource{
			ID:        id,
			Type:      resType,
			Name:      name,
			NodeIP:    nodeIP,
			Version:   version,
			Runtime:   runtime,
			Status:    status,
			CreatedAt: now,
			UpdatedAt: now,
			Metadata:  meta,
		})
	}
	saveState(s)
}

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Track deployed resources (services, databases, schedules)",
	}

	showCmd := &cobra.Command{
		Use:   "show [--type TYPE] [--node IP]",
		Short: "Show all tracked resources",
		RunE: func(c *cobra.Command, args []string) error {
			filterType, _ := c.Flags().GetString("type")
			filterNode, _ := c.Flags().GetString("node")

			s, err := loadState()
			if err != nil {
				return fmt.Errorf("load state: %w", err)
			}
			if len(s.Resources) == 0 {
				fmt.Println("  No resources tracked. Deploy something first or run:")
				fmt.Println("    sdk-ops state sync")
				return nil
			}

			// Filter and sort
			var rows []Resource
			for _, r := range s.Resources {
				if filterType != "" && r.Type != filterType {
					continue
				}
				if filterNode != "" && r.NodeIP != filterNode {
					continue
				}
				rows = append(rows, r)
			}
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].Type != rows[j].Type {
					return rows[i].Type < rows[j].Type
				}
				return rows[i].Name < rows[j].Name
			})

			if len(rows) == 0 {
				fmt.Println("  No matching resources.")
				return nil
			}

			fmt.Printf("  %s  %-20s %-15s %-10s %-10s  %s\n", colorBold, "NAME", "NODE", "TYPE", "STATUS", "VERSION")
			fmt.Println(strings.Repeat("  ", 1) + strings.Repeat("─", 75))
			for _, r := range rows {
				statusColor := colorGreen
			switch r.Status {
			case "unhealthy", "error":
				statusColor = colorRed
			case "warning":
				statusColor = colorYellow
			}
				ver := r.Version
				if ver == "" {
					ver = "-"
				}
				fmt.Printf("  %s%-20s %-15s %-10s %s%-10s%s %s\n",
					colorReset, r.Name, r.NodeIP, r.Type,
					statusColor, r.Status, colorReset, ver)
			}
			return nil
		},
	}
	showCmd.Flags().String("type", "", "Filter by type (service, database, backup_schedule)")
	showCmd.Flags().StringP("node", "n", "", "Filter by node IP")

	syncCmd := &cobra.Command{
		Use:   "sync [--node IP]",
		Short: "Scan nodes and synchronize tracked state",
		Long: `Connect to registered nodes and discover deployed services,
databases, and backup schedules, updating the state file.`,
		RunE: func(c *cobra.Command, args []string) error {
			filterNode, _ := c.Flags().GetString("node")
			user, _ := c.Flags().GetString("user")
			key, _ := c.Flags().GetString("key")
			port, _ := c.Flags().GetInt("port")

			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			type nodeTask struct {
				ip   string
				user string
				key  string
				port int
			}
			var nodes []nodeTask
			if filterNode != "" {
				u, k, p := user, key, port
				if n := lookupNode(filterNode); n != nil {
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
				nodes = append(nodes, nodeTask{filterNode, u, k, p})
			} else {
				for _, n := range cfg.Nodes {
					u, k, p := n.User, n.Key, n.Port
					if u == "" {
						u = "root"
					}
					if p == 0 {
						p = 22
					}
					nodes = append(nodes, nodeTask{n.IP, u, k, p})
				}
			}

			if len(nodes) == 0 {
				return fmt.Errorf("no nodes registered or specified")
			}

			var mu sync.Mutex
			var wg sync.WaitGroup
			discovered := 0
			for _, nt := range nodes {
				wg.Add(1)
				go func(nt nodeTask) {
					defer wg.Done()
					client := newSSHClient(nt.ip, nt.user, nt.port, nt.key)
					conn, err := client.Connect()
					if err != nil {
						fmt.Printf("  %s✗ %s: ssh failed%s\n", colorRed, nt.ip, colorReset)
						return
					}
					defer conn.Close()

					// Detect runtime
					dockerOK := ""
					k3sOK := ""
					if out, _, _ := ssh.Run(conn, "command -v docker && docker --version 2>/dev/null | head -1 || echo ''"); out != "" {
						dockerOK = "docker"
					}
					if out, _, _ := ssh.Run(conn, "command -v k3s && k3s --version 2>/dev/null | head -1 || echo ''"); out != "" {
						k3sOK = "k3s"
					}

					// Services
					services, _ := deploy.ListServices(conn)
					for _, svc := range services {
						status := "ok"
						if s, _ := deploy.ServiceStatus(conn, svc); s != "" {
							if strings.Contains(s, "running") || strings.Contains(s, "type:") {
								status = "ok"
							} else {
								status = "error"
							}
						}
						runtime := dockerOK
						if k3sOK != "" {
							runtime = k3sOK
						}
						mu.Lock()
						stateRecord("service", svc, nt.ip, "synced", runtime, status, nil)
						discovered++
						mu.Unlock()
					}

					// Running containers (possible databases)
					dbContainers, _, _ := ssh.Run(conn, `docker ps --format '{{.Names}} {{.Image}}' 2>/dev/null | grep -E '(postgres|mysql|redis|mongo)' || true`)
					for line := range strings.SplitSeq(dbContainers, "\n") {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}
						parts := strings.Fields(line)
						if len(parts) >= 2 {
							name := parts[0]
							img := parts[1]
							dbType := "database"
							if strings.Contains(img, "postgres") || strings.Contains(img, "mysql") || strings.Contains(img, "mariadb") || strings.Contains(img, "redis") || strings.Contains(img, "mongo") {
								dbType = "database"
							}
							mu.Lock()
							stateRecord(dbType, name, nt.ip, "synced", "docker", "ok", map[string]string{"image": img})
							discovered++
							mu.Unlock()
						}
					}

					fmt.Printf("  %s✓ %s: %d resources%s\n", colorGreen, nt.ip, len(services)+strings.Count(dbContainers, "\n"), colorReset)
				}(nt)
			}
			wg.Wait()

			if discovered > 0 {
				fmt.Printf("\n  %s%d resources synced%s\n", colorGreen, discovered, colorReset)
			} else {
				fmt.Println("\n  No resources discovered.")
			}
			return nil
		},
	}
	syncCmd.Flags().StringP("node", "n", "", "Node IP (default: all registered)")
	syncCmd.Flags().StringP("user", "u", "root", "SSH user")
	syncCmd.Flags().StringP("key", "k", "", "SSH private key path")
	syncCmd.Flags().IntP("port", "p", 22, "SSH port")

	cmd.AddCommand(showCmd)
	cmd.AddCommand(syncCmd)
	return cmd
}
