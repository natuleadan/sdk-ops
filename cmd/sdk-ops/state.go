package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	goss "golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"

	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/ssh"
)

type Resource struct {
	ID        string            `yaml:"id"`
	Type      string            `yaml:"type"`
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
	if err := saveState(s); err != nil { log.Printf("state: save error: %v", err) }
}

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Track deployed resources (services, databases, schedules)",
	}

	cmd.AddCommand(newStateShowCmd())
	cmd.AddCommand(newStateSyncCmd())
	return cmd
}

func newStateShowCmd() *cobra.Command {
	cmd := &cobra.Command{
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

	cmd.Flags().String("type", "", "Filter by type (service, database, backup_schedule)")
	cmd.Flags().StringP("node", "n", "", "Filter by node IP")
	return cmd
}

func newStateSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [--node IP]",
		Short: "Scan nodes and synchronize tracked state",
		Long: `Connect to registered nodes and discover deployed services,
databases, and backup schedules, updating the state file.`,
		RunE: stateSyncRunE,
	}

	cmd.Flags().StringP("node", "n", "", "Node IP (default: all registered)")
	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

type syncNodeTask struct {
	ip   string
	user string
	key  string
	port int
}

func buildSyncNodeList(cfg *RootConfig, filterNode, user, key string, port int) []syncNodeTask {
	var nodes []syncNodeTask
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
		nodes = append(nodes, syncNodeTask{filterNode, u, k, p})
	} else {
		for _, n := range cfg.Nodes {
			u, k, p := n.User, n.Key, n.Port
			if u == "" {
				u = "root"
			}
			if p == 0 {
				p = 22
			}
			nodes = append(nodes, syncNodeTask{n.IP, u, k, p})
		}
	}
	return nodes
}

func syncSingleNode(nt syncNodeTask, mu *sync.Mutex) int {
	client := newSSHClient(nt.ip, nt.user, nt.port, nt.key)
	conn, err := client.Connect()
	if err != nil {
		fmt.Printf("  %s✗ %s: ssh failed%s\n", colorRed, nt.ip, colorReset)
		return 0
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "state: conn close error: %v\n", err) } }()

	dockerOK := ""
	k3sOK := ""
	if out, _, _ := ssh.Run(conn, "command -v docker && docker --version 2>/dev/null | head -1 || echo ''"); out != "" {
		dockerOK = "docker"
	}
	if out, _, _ := ssh.Run(conn, "command -v k3s && k3s --version 2>/dev/null | head -1 || echo ''"); out != "" {
		k3sOK = "k3s"
	}

	serviceCount := stateSyncServices(conn, nt.ip, dockerOK, k3sOK, mu)
	dbCount := stateSyncDatabases(conn, nt.ip, mu)
	total := serviceCount + dbCount

	fmt.Printf("  %s✓ %s: %d resources%s\n", colorGreen, nt.ip, total, colorReset)
	return total
}

func stateSyncRunE(c *cobra.Command, args []string) error {
	filterNode, _ := c.Flags().GetString("node")
	user, _ := c.Flags().GetString("user")
	key, _ := c.Flags().GetString("key")
	port, _ := c.Flags().GetInt("port")

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	nodes := buildSyncNodeList(cfg, filterNode, user, key, port)
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes registered or specified")
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	counts := make(chan int, len(nodes))
	for _, nt := range nodes {
		wg.Add(1)
		go func(nt syncNodeTask) {
			defer wg.Done()
			counts <- syncSingleNode(nt, &mu)
		}(nt)
	}
	wg.Wait()
	close(counts)

	discovered := 0
	for c := range counts {
		discovered += c
	}

	if discovered > 0 {
		fmt.Printf("\n  %s%d resources synced%s\n", colorGreen, discovered, colorReset)
	} else {
		fmt.Println("\n  No resources discovered.")
	}
	return nil
}

func stateSyncServices(conn *goss.Client, ip, dockerOK, k3sOK string, mu *sync.Mutex) int {
	count := 0
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
		stateRecord("service", svc, ip, "synced", runtime, status, nil)
		count++
		mu.Unlock()
	}
	return count
}

func stateSyncDatabases(conn *goss.Client, ip string, mu *sync.Mutex) int {
	count := 0
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
			mu.Lock()
			stateRecord("database", name, ip, "synced", "docker", "ok", map[string]string{"image": img})
			count++
			mu.Unlock()
		}
	}
	return count
}
