package plan

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/k3s"
	"github.com/natuleadan/sdk-ops/ssh"
	"gopkg.in/yaml.v3"
)

type ApplyResult struct {
	Host   string
	Step   string
	Error  error
}

func connectHost(h Host, insecure bool) (*goss.Client, error) {
	opts := []ssh.Option{ssh.WithPort(h.Port)}
	if h.SSHKey != "" {
		opts = append(opts, ssh.WithKey(h.SSHKey))
	}
	if insecure {
		opts = append(opts, ssh.WithInsecure())
	}
	c := ssh.New(h.Host, h.User, opts...)
	return c.Connect()
}

func Apply(p *Plan, insecure bool) []ApplyResult {
	results := make(chan ApplyResult, len(p.Hosts))
	var wg sync.WaitGroup

	conns := verifySSH(p, insecure, &wg, results)
	if conns == nil {
		close(results)
		return collectResults(results)
	}
	defer closeConns(conns)

	firstServer, firstConn := findFirstServer(p, conns)
	token, arch := getK3sState(firstConn, firstServer, p.Mode)

	if p.Mode == "k3s" {
		installK3sServers(p, conns, firstServer, firstConn, &token, &arch, &wg, results)
		joinAgents(p, conns, firstServer, token, &wg, results)
	}

	registerNodes(p, conns, arch)

	close(results)
	return collectResults(results)
}

func verifySSH(p *Plan, insecure bool, wg *sync.WaitGroup, results chan<- ApplyResult) map[string]*goss.Client {
	fmt.Println("\n🔍 Verifying SSH access to all hosts...")
	sshErrs := 0
	var mu sync.Mutex
	conns := make(map[string]*goss.Client)

	for _, h := range p.Hosts {
		if p.Mode != "k3s" && h.Role == "agent" {
			continue
		}
		wg.Add(1)
		go func(host Host) {
			defer wg.Done()
			conn, err := connectHost(host, insecure)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fmt.Printf("  ✗ %s (%s): SSH failed: %v\n", host.Name, host.Host, err)
				sshErrs++
				results <- ApplyResult{Host: host.Name, Step: "ssh", Error: err}
			} else {
				fmt.Printf("  ✓ %s (%s): SSH OK\n", host.Name, host.Host)
				conns[host.Name] = conn
			}
		}(h)
	}
	wg.Wait()

	if sshErrs > 0 {
		fmt.Printf("\n⚠ %d hosts failed SSH check. Aborting.\n", sshErrs)
		closeConns(conns)
		return nil
	}
	return conns
}

func closeConns(conns map[string]*goss.Client) {
	for _, c := range conns {
		if err := c.Close(); err != nil {
			log.Printf("plan: conn close error: %v", err)
		}
	}
}

func findFirstServer(p *Plan, conns map[string]*goss.Client) (*Host, *goss.Client) {
	for _, h := range p.Hosts {
		if h.Role == "server" {
			host := h
			return &host, conns[h.Name]
		}
	}
	return nil, nil
}

func getK3sState(firstConn *goss.Client, firstServer *Host, mode string) (token, arch string) {
	if mode != "k3s" || firstConn == nil {
		return "", ""
	}
	checkOut, _, _ := ssh.Run(firstConn, "command -v k3s && echo 'installed' || echo 'missing'")
	if strings.Contains(checkOut, "installed") {
		fmt.Printf("\n  → k3s already installed on %s, fetching token...\n", firstServer.Name)
		tok, _, err := ssh.Run(firstConn, "sudo cat /var/lib/rancher/k3s/server/token 2>/dev/null || true")
		if err == nil {
			token = strings.TrimSpace(tok)
		}
		archOut, _, _ := ssh.Run(firstConn, "uname -m")
		arch = strings.TrimSpace(archOut)
	}
	return token, arch
}

func installK3sServers(p *Plan, conns map[string]*goss.Client, firstServer *Host, firstConn *goss.Client, token, arch *string, wg *sync.WaitGroup, results chan<- ApplyResult) {
	if firstConn == nil {
		return
	}

	fmt.Printf("\n🚀 Installing k3s on first server %s (%s)...\n", firstServer.Name, firstServer.Host)
	checkOut, _, _ := ssh.Run(firstConn, "command -v k3s && echo 'installed' || echo 'missing'")
	if !strings.Contains(checkOut, "installed") {
		err := installK3s(firstConn, firstServer.Host, p.ServerOptions)
		if err != nil {
			results <- ApplyResult{Host: firstServer.Name, Step: "install", Error: err}
		} else {
			results <- ApplyResult{Host: firstServer.Name, Step: "install", Error: nil}
		}
	} else {
		fmt.Printf("  → k3s already installed on %s\n", firstServer.Name)
		results <- ApplyResult{Host: firstServer.Name, Step: "install", Error: nil}
	}

	tok, _, _ := ssh.Run(firstConn, "sudo cat /var/lib/rancher/k3s/server/token")
	if tok != "" {
		*token = strings.TrimSpace(tok)
	}
	archOut, _, _ := ssh.Run(firstConn, "uname -m")
	*arch = strings.TrimSpace(archOut)

	var remainingServers []Host
	started := false
	for _, h := range p.Hosts {
		if h.Role == "server" {
			if !started {
				started = true
				continue
			}
			remainingServers = append(remainingServers, h)
		}
	}

	if len(remainingServers) > 0 {
		fmt.Printf("\n🚀 Installing remaining %d servers in parallel...\n", len(remainingServers))
		sem := make(chan struct{}, p.Parallel)
		for _, h := range remainingServers {
			sem <- struct{}{}
			wg.Add(1)
			go func(host Host, conn *goss.Client) {
				defer wg.Done()
				defer func() { <-sem }()
				installServer(host, conn, p, results)
			}(h, conns[h.Name])
		}
		wg.Wait()
	}
}

func installServer(host Host, conn *goss.Client, p *Plan, results chan<- ApplyResult) {
	checkOut, _, _ := ssh.Run(conn, "command -v k3s && echo 'installed' || echo 'missing'")
	if !strings.Contains(checkOut, "installed") {
		installCfg := k3s.InstallConfig{
			PublicIP:       host.Host,
			ExtraArgs:      p.ServerOptions.K3sExtraArgs,
			K3sChannel:     p.ServerOptions.K3sChannel,
			K3sVersion:     p.ServerOptions.K3sVersion,
			DisableTraefik: p.ServerOptions.DisableTraefik,
		}
		err := k3s.Install(conn, installCfg)
		if err != nil {
			results <- ApplyResult{Host: host.Name, Step: "install", Error: err}
			return
		}
	} else {
		fmt.Printf("  → k3s already installed on %s\n", host.Name)
	}
	results <- ApplyResult{Host: host.Name, Step: "install", Error: nil}
}

func joinAgents(p *Plan, conns map[string]*goss.Client, firstServer *Host, token string, wg *sync.WaitGroup, results chan<- ApplyResult) {
	var agents []Host
	for _, h := range p.Hosts {
		if h.Role == "agent" {
			agents = append(agents, h)
		}
	}

	if len(agents) == 0 || token == "" {
		return
	}

	fmt.Printf("\n🔗 Joining %d agents in parallel...\n", len(agents))
	sem := make(chan struct{}, p.Parallel)
	for _, h := range agents {
		sem <- struct{}{}
		wg.Add(1)
		go func(host Host, conn *goss.Client) {
			defer wg.Done()
			defer func() { <-sem }()
			joinCfg := k3s.JoinConfig{
				ServerIP:   firstServer.Host,
				Token:      token,
				ExtraArgs:  p.AgentOptions.K3sExtraArgs,
				K3sChannel: p.AgentOptions.K3sChannel,
				K3sVersion: p.AgentOptions.K3sVersion,
			}
			err := k3s.Join(conn, conn, joinCfg)
			if err != nil {
				results <- ApplyResult{Host: host.Name, Step: "join", Error: err}
				return
			}
			results <- ApplyResult{Host: host.Name, Step: "join", Error: nil}
		}(h, conns[h.Name])
	}
	wg.Wait()
}

func registerNodes(p *Plan, conns map[string]*goss.Client, arch string) {
	fmt.Println("\n📝 Registering nodes in local config...")
	var nodeCfgs []NodeConfig
	for _, h := range p.Hosts {
		nodeArch := arch
		if h.Role == "agent" {
			if c, ok := conns[h.Name]; ok {
				a, _, _ := ssh.Run(c, "uname -m")
				nodeArch = strings.TrimSpace(a)
			}
		}
		nc := NodeConfig{
			IP:   h.Host,
			User: h.User,
			Key:  h.SSHKey,
			Port: h.Port,
			Mode: p.Mode,
			Role: h.Role,
			Arch: nodeArch,
		}
		nodeCfgs = append(nodeCfgs, nc)
		fmt.Printf("  ✓ %s (%s) registered\n", h.Name, h.Host)
	}
	saveNodes(nodeCfgs)
}

func collectResults(results <-chan ApplyResult) []ApplyResult {
	var out []ApplyResult
	for r := range results {
		out = append(out, r)
	}
	return out
}

func installK3s(conn *goss.Client, publicIP string, opts Options) error {
	cfg := k3s.InstallConfig{
		PublicIP:       publicIP,
		ExtraArgs:      opts.K3sExtraArgs,
		K3sChannel:     opts.K3sChannel,
		K3sVersion:     opts.K3sVersion,
		DisableTraefik: opts.DisableTraefik,
	}
	return k3s.Install(conn, cfg)
}

type NodeConfig struct {
	IP       string `yaml:"ip"`
	User     string `yaml:"user"`
	Key      string `yaml:"key,omitempty"`
	Port     int    `yaml:"port"`
	Mode     string `yaml:"mode,omitempty"`
	Role     string `yaml:"role,omitempty"`
	Arch     string `yaml:"arch,omitempty"`
}

type RootConfig struct {
	Nodes []NodeConfig `yaml:"nodes"`
}

func configDir() string {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".sdk-ops")
	func() { if err := os.MkdirAll(dir, 0700); err != nil { fmt.Fprintf(os.Stderr, "mkdir: %v\n", err) } }()
	return dir
}

func configPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func saveNodes(nodes []NodeConfig) {
	cfg := RootConfig{Nodes: nodes}
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath(), data, 0600); err != nil { log.Printf("plan: write config error: %v", err) }
}
