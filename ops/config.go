package ops

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host    string `yaml:"host"`
	User    string `yaml:"user"`
	SSHKey  string `yaml:"ssh_key"`
	SSHPort int    `yaml:"ssh_port"`
	Mode    string `yaml:"mode"`
}

func LoadConfig(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// Expand env vars
	data = []byte(os.Expand(string(data), os.Getenv))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("host is required in %s", path)
	}
	if cfg.User == "" {
		cfg.User = "root"
	}
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 22
	}
	if cfg.Mode == "" {
		cfg.Mode = "k3s"
	}
	if cfg.SSHKey == "" {
		homeDir, _ := os.UserHomeDir()
		cfg.SSHKey = filepath.Join(homeDir, ".ssh", "id_ed25519")
	}

	mode := Mode(cfg.Mode)
	switch mode {
	case ModeK3s, ModeDocker, ModeBare:
	default:
		return nil, fmt.Errorf("invalid mode %q: must be k3s, docker, or bare", cfg.Mode)
	}

	return &ServerConfig{
		Host:    cfg.Host,
		User:    cfg.User,
		SSHKey:  cfg.SSHKey,
		SSHPort: cfg.SSHPort,
		Mode:    mode,
	}, nil
}
