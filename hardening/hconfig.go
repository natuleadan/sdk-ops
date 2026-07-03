package hardening

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Node      NodeInfo     `yaml:"node"`
	Hardening HardeningLog `yaml:"hardening,omitempty"`
}

type NodeInfo struct {
	IP       string `yaml:"ip"`
	Hostname string `yaml:"hostname,omitempty"`
	OS       string `yaml:"os,omitempty"`
	User     string `yaml:"user"`
	SSHPort  int    `yaml:"ssh_port"`
	SSHKey   string `yaml:"ssh_key,omitempty"`
	Mode     string `yaml:"mode,omitempty"` // k3s, docker, bare
}

type HardeningLog struct {
	Applied bool     `yaml:"applied"`
	Date    string   `yaml:"date,omitempty"`
	Steps   []string `yaml:"steps,omitempty"`
}

func ConfigDir() string {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".sdk-ops", "servers")
	if err := os.MkdirAll(dir, 0700); err != nil { log.Printf("mkdir: %v", err) }
	return dir
}

func ConfigPath(ip string) string {
	return filepath.Join(ConfigDir(), ip+".yaml")
}

func SaveServerConfig(cfg ServerConfig) error {
	cfg.Hardening.Date = time.Now().UTC().Format(time.RFC3339)
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(cfg.Node.IP), data, 0600)
}

func LoadServerConfig(ip string) (*ServerConfig, error) {
	data, err := os.ReadFile(ConfigPath(ip))
	if err != nil {
		return nil, err
	}
	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ExportConfig(ip, hostname, osName, user, key, mode string, sshPort int) ServerConfig {
	return ServerConfig{
		Node: NodeInfo{
			IP:       ip,
			Hostname: hostname,
			OS:       osName,
			User:     user,
			SSHPort:  sshPort,
			SSHKey:   key,
			Mode:     mode,
		},
		Hardening: HardeningLog{
			Applied: true,
			Date:    time.Now().UTC().Format(time.RFC3339),
			Steps:   []string{"install_packages", "create_user", "kernel_tuning", "fail2ban", "ssh_hardening", "nftables"},
		},
	}
}

func (c *ServerConfig) Print() {
	fmt.Printf("  Node: %s (%s)\n", c.Node.IP, c.Node.Hostname)
	fmt.Printf("  OS:   %s\n", c.Node.OS)
	fmt.Printf("  User: %s  Port: %d  Mode: %s\n", c.Node.User, c.Node.SSHPort, c.Node.Mode)
	fmt.Printf("  Hardening: %v (%s)\n", c.Hardening.Applied, c.Hardening.Date)
}
