package sdk_ops

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/cloudinit"
	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/docker"
	"github.com/natuleadan/sdk-ops/hardening"
	"github.com/natuleadan/sdk-ops/k3s"
	"github.com/natuleadan/sdk-ops/providers"
	"github.com/natuleadan/sdk-ops/ssh"
)

type Mode string

const (
	ModeK3s    Mode = "k3s"
	ModeDocker Mode = "docker"
	ModeBare   Mode = "bare"
)

type ServerConfig struct {
	Host        string
	User        string
	SSHKey      string
	SSHPort     int
	Mode        Mode
	CrowdSec    bool
	Monitor     bool
	LockRoot    bool
	HardSSHPort int
	Kubeconfig  string
	Context     string
	MergeKube   bool
	InsecureSSH bool
	CloudInit   bool
	Provider    providers.Provider
}

type Server struct {
	cfg  ServerConfig
	conn *gossh.Client
}

func New(cfg ServerConfig) *Server {
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 22
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	return &Server{cfg: cfg}
}

func (s *Server) CreateVPS(ctx context.Context, createCfg providers.VPSCreateConfig) (*providers.VPS, error) {
	if s.cfg.Provider == nil {
		return nil, fmt.Errorf("no provider configured")
	}
	vps, err := s.cfg.Provider.CreateVPS(ctx, createCfg)
	if err != nil {
		return nil, fmt.Errorf("create vps: %w", err)
	}
	s.cfg.Host = vps.IP
	return vps, nil
}

func (s *Server) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	if s.cfg.Provider == nil {
		return nil, fmt.Errorf("no provider configured")
	}
	return s.cfg.Provider.CreateK8s(ctx, cfg)
}

func (s *Server) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	if s.cfg.Provider == nil {
		return nil, fmt.Errorf("no provider configured")
	}
	return s.cfg.Provider.CreateLB(ctx, cfg)
}

func (s *Server) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	if s.cfg.Provider == nil {
		return fmt.Errorf("no provider configured")
	}
	return s.cfg.Provider.CreateDNSRecord(ctx, zoneID, r)
}

func (s *Server) Destroy(ctx context.Context) error {
	if s.cfg.Provider == nil || s.cfg.Host == "" {
		return fmt.Errorf("no provider or host to destroy")
	}
	return s.cfg.Provider.DeleteVPS(ctx, s.cfg.Host)
}

func (s *Server) connect() error {
	if s.conn != nil {
		return nil
	}
	opts := []ssh.Option{ssh.WithPort(s.cfg.SSHPort)}
	if s.cfg.SSHKey != "" {
		opts = append(opts, ssh.WithKey(s.cfg.SSHKey))
	}
	if s.cfg.InsecureSSH {
		opts = append(opts, ssh.WithInsecure())
	}
	client := ssh.New(s.cfg.Host, s.cfg.User, opts...)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	s.conn = conn
	return nil
}

func (s *Server) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *Server) Provision(ctx context.Context) error {
	if s.cfg.CloudInit && s.cfg.Provider != nil {
		return s.provisionCloudInit(ctx)
	}
	return s.provisionSSH(ctx)
}

func ensureDirectories(s *Server) error {
	if _, _, err := ssh.Run(s.conn, `mkdir -p /opt/sdk-ops/services /opt/sdk-ops/backups /opt/sdk-ops/logs && echo "sdk-ops-init" > /opt/sdk-ops/.version`); err != nil {
		log.Printf("server: ssh run error: %v", err)
	}
	return nil
}

func (s *Server) provisionSSH(_ context.Context) error {
	if err := s.connect(); err != nil {
		return err
	}

	if err := applyHardening(s); err != nil {
		return err
	}

	if err := installRuntime(s); err != nil {
		return err
	}

	if err := ensureDirectories(s); err != nil {
		return err
	}

	return nil
}

func applyHardening(s *Server) error {
	hardCfg := hardening.DefaultConfig()
	if s.cfg.User != "root" {
		hardCfg.User = s.cfg.User
	}
	hardCfg.EnableMonitor = s.cfg.Monitor
	hardCfg.LockRoot = s.cfg.LockRoot
	if s.cfg.HardSSHPort > 0 {
		hardCfg.SSHPort = s.cfg.HardSSHPort
	}
	if err := hardening.Apply(s.conn, hardCfg); err != nil {
		fmt.Printf("  ⚠️  Hardening partially failed, continuing...\n")
	}
	if err := s.conn.Close(); err != nil {
		log.Printf("server: conn close error: %v", err)
	}
	s.conn = nil

	reconnectPort := s.cfg.SSHPort
	reconnectUser := hardCfg.User
	if hardCfg.MigrateSSH() {
		reconnectPort = hardCfg.SSHPort
	}
	return reconnectAfterHardening(s, reconnectUser, reconnectPort)
}

func reconnectAfterHardening(s *Server, user string, port int) error {
	fmt.Printf("  → Reconnecting as %s@%s port %d...\n", user, s.cfg.Host, port)
	for attempt := 1; attempt <= 10; attempt++ {
		opts := []ssh.Option{ssh.WithPort(port)}
		if s.cfg.SSHKey != "" {
			opts = append(opts, ssh.WithKey(s.cfg.SSHKey))
		}
		if s.cfg.InsecureSSH {
			opts = append(opts, ssh.WithInsecure())
		}
		client := ssh.New(s.cfg.Host, user, opts...)
		conn, err := client.Connect()
		if err == nil {
			s.conn = conn
			return nil
		}
		if attempt == 10 {
			return fmt.Errorf("reconnect after hardening: %w", err)
		}
		fmt.Printf("  Waiting for SSH on port %d... (attempt %d/10)\n", port, attempt)
		time.Sleep(3 * time.Second)
	}
	return nil
}

func installRuntime(s *Server) error {
	if s.cfg.CrowdSec {
		if err := installCrowdSec(s.conn); err != nil {
			return err
		}
	}

	if s.cfg.Mode == ModeDocker || s.cfg.Mode == ModeK3s {
		if err := docker.Install(s.conn); err != nil {
			return err
		}
	}

	if s.cfg.Mode == ModeK3s {
		installCfg := k3s.DefaultInstallConfig(s.cfg.Host)
		installCfg.LocalPath = s.cfg.Kubeconfig
		installCfg.Context = s.cfg.Context
		installCfg.Merge = s.cfg.MergeKube
		if err := k3s.Install(s.conn, installCfg); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) provisionCloudInit(ctx context.Context) error {
	ciCfg := cloudinit.DefaultConfig()
	ciCfg.Mode = string(s.cfg.Mode)
	ciCfg.CrowdSec = s.cfg.CrowdSec
	ciCfg.EnableMonitor = s.cfg.Monitor
	userData := cloudinit.Generate(ciCfg)

	vpsCfg := providers.VPSCreateConfig{
		UserData: userData,
	}
	_, err := s.cfg.Provider.CreateVPS(ctx, vpsCfg)
	if err != nil {
		return fmt.Errorf("cloud-init create vps: %w", err)
	}

	time.Sleep(10 * time.Second)
	ciUser := "sdkops"
	ciPort := 2222
	for attempt := 1; attempt <= 30; attempt++ {
		opts := []ssh.Option{ssh.WithPort(ciPort)}
		if s.cfg.InsecureSSH {
			opts = append(opts, ssh.WithInsecure())
		}
		client := ssh.New(s.cfg.Host, ciUser, opts...)
		conn, err := client.Connect()
		if err == nil {
			if err := conn.Close(); err != nil {
				log.Printf("server: conn close error: %v", err)
			}
			s.cfg.User = ciUser
			s.cfg.SSHPort = ciPort
			return nil
		}
		if attempt == 30 {
			return fmt.Errorf("cloud-init: VPS not ready after 150s")
		}
		time.Sleep(5 * time.Second)
	}
	return nil
}

func installCrowdSec(client *gossh.Client) error {
	fmt.Println("  → Installing CrowdSec...")
	script := `#!/bin/bash
set -euo pipefail
if command -v cscli &>/dev/null; then
    echo "CrowdSec already installed"
    exit 0
fi
curl -fsSL https://install.crowdsec.net | sh
systemctl enable crowdsec
systemctl start crowdsec
echo "CrowdSec installed"
`
	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("crowdsec install failed: %w\noutput: %s", err, out)
	}
	fmt.Print(out)
	return nil
}

func (s *Server) Status() (string, error) {
	if err := s.connect(); err != nil {
		return "", err
	}
	defer func() { if err := s.conn.Close(); err != nil { log.Printf("server: conn close error: %v", err) } }()

	var result string
	sysInfo := `echo "Hostname: $(hostname)"
echo "Kernel:   $(uname -r)"
echo "Uptime:   $(uptime -p)"
echo "CPU:      $(nproc) cores, load: $(uptime | awk -F'load average:' '{print $2}')"
echo "Memory:   $(free -h | awk '/^Mem:/ {print $3 "/" $2}')"
echo "Disk:     $(df -h / | awk 'NR==2 {print $3 "/" $2}')"`
	out, _, err := ssh.Run(s.conn, sysInfo)
	if err != nil {
		return "", fmt.Errorf("system info: %w", err)
	}
	result += out

	hardenOut, err := hardening.Check(s.conn)
	if err == nil {
		result += hardenOut
	}

	dockerOut, err := docker.Check(s.conn)
	if err == nil {
		result += dockerOut
	}

	k3sOut, err := k3s.Check(s.conn)
	if err == nil {
		result += k3sOut
	}

	return result, nil
}

func (s *Server) Deploy(sourceDir string) (*deploy.DeployResult, error) {
	if err := s.connect(); err != nil {
		return nil, err
	}
	defer func() { if err := s.conn.Close(); err != nil { log.Printf("server: conn close error: %v", err) } }()

	cfg := deploy.UploadConfig{
		ServiceName: filepath.Base(sourceDir),
		SourceDir:   sourceDir,
	}
	return deploy.UploadAndDeploy(s.conn, cfg)
}

func (s *Server) DeployPush(sourceDir, name string) (*deploy.DeployResult, error) {
	if err := s.connect(); err != nil {
		return nil, err
	}
	defer func() { if err := s.conn.Close(); err != nil { log.Printf("server: conn close error: %v", err) } }()

	reg := deploy.DefaultRegistry()
	if _, err := deploy.BuildAndPushImage(sourceDir, name, reg); err != nil {
		fmt.Printf("  ⚠️  Docker build+push failed: %v\n", err)
	}

	cfg := deploy.UploadConfig{
		ServiceName: name,
		SourceDir:   sourceDir,
		Files:       []string{"docker-compose.yml", "service.yaml"},
		Exclude:     []string{".git", "node_modules", ".env"},
	}

	result, err := deploy.UploadAndDeploy(s.conn, cfg)
	if err != nil {
		return nil, err
	}

	svcCfg := deploy.ServiceConfig{Name: name, HealthTimeout: 30}
	if err := deploy.RunService(s.conn, svcCfg); err != nil {
		return nil, err
	}

	if err := deploy.HealthCheck(s.conn, name, 30, ""); err != nil {
		fmt.Printf("\n  ⚠️  Health check failed, rolling back...\n")
		if rbErr := deploy.Rollback(s.conn, name, ""); rbErr != nil {
			return nil, fmt.Errorf("health: %v\nrollback also failed: %v", err, rbErr)
		}
		if err := deploy.RunService(s.conn, svcCfg); err != nil {
			log.Printf("server: run service error: %v", err)
		}
		return nil, fmt.Errorf("health check failed, rolled back")
	}

	return result, nil
}

func (s *Server) Exec(cmd string) (string, error) {
	if err := s.connect(); err != nil {
		return "", err
	}
	defer func() { if err := s.conn.Close(); err != nil { log.Printf("server: conn close error: %v", err) } }()

	out, _, err := ssh.Run(s.conn, cmd)
	return out, err
}

func (s *Server) BackupServices(destDir string) (string, error) {
	if err := s.connect(); err != nil {
		return "", err
	}
	defer func() { if err := s.conn.Close(); err != nil { log.Printf("server: conn close error: %v", err) } }()

	return deploy.BackupServices(s.conn, destDir)
}

func (s *Server) RestoreServices(backupPath string) error {
	if err := s.connect(); err != nil {
		return err
	}
	defer func() { if err := s.conn.Close(); err != nil { log.Printf("server: conn close error: %v", err) } }()

	return deploy.RestoreServices(s.conn, backupPath)
}

type ClusterClient struct {
	server *Server
}

func (s *Server) Cluster() *ClusterClient {
	return &ClusterClient{server: s}
}

func (c *ClusterClient) exec(kubectlCmd string) (string, error) {
	return c.server.Exec("KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl " + kubectlCmd)
}

func (c *ClusterClient) Nodes() (string, error) {
	return c.exec("get nodes -o wide")
}

func (c *ClusterClient) Pods(namespace string) (string, error) {
	ns := "--all-namespaces"
	if namespace != "" {
		ns = "-n " + namespace
	}
	return c.exec("get pods " + ns)
}

func (c *ClusterClient) Services(namespace string) (string, error) {
	ns := "--all-namespaces"
	if namespace != "" {
		ns = "-n " + namespace
	}
	return c.exec("get services " + ns)
}

func (c *ClusterClient) Deployments(namespace string) (string, error) {
	ns := "--all-namespaces"
	if namespace != "" {
		ns = "-n " + namespace
	}
	return c.exec("get deployments " + ns)
}

func (c *ClusterClient) Ingresses(namespace string) (string, error) {
	ns := "--all-namespaces"
	if namespace != "" {
		ns = "-n " + namespace
	}
	return c.exec("get ingress " + ns)
}

func (c *ClusterClient) ConfigMaps(namespace string) (string, error) {
	ns := "--all-namespaces"
	if namespace != "" {
		ns = "-n " + namespace
	}
	return c.exec("get configmaps " + ns)
}

func (c *ClusterClient) Secrets(namespace string) (string, error) {
	ns := "--all-namespaces"
	if namespace != "" {
		ns = "-n " + namespace
	}
	return c.exec("get secrets " + ns)
}

func (c *ClusterClient) Info() (string, error) {
	return c.exec("cluster-info")
}

func (c *ClusterClient) Version() (string, error) {
	return c.exec("version")
}

func (c *ClusterClient) Top() (string, error) {
	nodes, _ := c.exec("top nodes")
	pods, _ := c.exec("top pods --all-namespaces")
	return nodes + "\n" + pods, nil
}

func (c *ClusterClient) Logs(pod string, follow bool) (string, error) {
	f := ""
	if follow {
		f = " -f"
	}
	return c.exec("logs " + pod + f)
}

func (c *ClusterClient) Exec(pod string, cmdArgs []string) (string, error) {
	var args strings.Builder
	for _, a := range cmdArgs {
		args.WriteString(" " + a)
	}
	return c.exec("exec -it " + pod + " --" + args.String())
}

func (c *ClusterClient) Scale(resource string, replicas int32) (string, error) {
	return c.exec(fmt.Sprintf("scale --replicas=%d %s", replicas, resource))
}

func (c *ClusterClient) Apply(file string) (string, error) {
	return c.exec("apply -f " + file)
}

func (c *ClusterClient) Delete(resource, name string) (string, error) {
	return c.exec("delete " + resource + " " + name)
}

func (c *ClusterClient) Describe(resource, name string) (string, error) {
	return c.exec("describe " + resource + " " + name)
}
