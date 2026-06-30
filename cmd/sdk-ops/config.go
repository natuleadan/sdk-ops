package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/natuleadan/sdk-ops/providers"
)

type NodeConfig struct {
	IP       string `yaml:"ip"`
	User     string `yaml:"user"`
	Key      string `yaml:"key,omitempty"`
	Port     int    `yaml:"port"`
	Mode     string `yaml:"mode,omitempty"` // k3s, docker, bare
	Role     string `yaml:"role,omitempty"` // server, agent
	Arch     string `yaml:"arch,omitempty"` // aarch64, x86_64
	Hostname string `yaml:"hostname,omitempty"`
}

type NLARootConfig struct {
	Nodes []NodeConfig `yaml:"nodes"`
}

func configDir() string {
	dir := os.ExpandEnv("$HOME/.sdk-ops")
	os.MkdirAll(dir, 0700)
	return dir
}

func configPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func loadConfig() (*NLARootConfig, error) {
	var cfg NLARootConfig
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func lookupNode(ip string) *NodeConfig {
	cfg, err := loadConfig()
	if err != nil {
		return nil
	}
	for _, n := range cfg.Nodes {
		if n.IP == ip {
			return &n
		}
	}
	return nil
}

func saveConfig(cfg *NLARootConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func newConfigCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "config",
		Short: "Manage sdk-ops configuration and registered nodes",
	}

	var initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.sdk-ops/config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("  Created %s\n", configPath())
			return nil
		},
	}

	var addCmd = &cobra.Command{
		Use:   "add-node <ip> [--user] [--key] [--port]",
		Short: "Register a node in ~/.sdk-ops/config.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			user, _ := cmd.Flags().GetString("user")
			key, _ := cmd.Flags().GetString("key")
			port, _ := cmd.Flags().GetInt("port")
			mode, _ := cmd.Flags().GetString("mode")

			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			// Check for duplicate
			for i, n := range cfg.Nodes {
				if n.IP == args[0] {
					cfg.Nodes[i].User = user
					cfg.Nodes[i].Key = key
					cfg.Nodes[i].Port = port
					cfg.Nodes[i].Mode = mode
					if err := saveConfig(cfg); err != nil {
						return err
					}
					fmt.Printf("  Updated node %s\n", args[0])
					return nil
				}
			}

			cfg.Nodes = append(cfg.Nodes, NodeConfig{
				IP:   args[0],
				User: user,
				Key:  key,
				Port: port,
				Mode: mode,
			})

			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("  Added node %s\n", args[0])
			return nil
		},
	}
	addCmd.Flags().String("user", "root", "SSH user")
	addCmd.Flags().String("key", "", "SSH key path")
	addCmd.Flags().Int("port", 22, "SSH port")
	addCmd.Flags().String("mode", "", "Installation mode (k3s, docker, bare)")

	var listCmd = &cobra.Command{
		Use:   "list-nodes",
		Short: "List all registered nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if len(cfg.Nodes) == 0 {
				fmt.Println("  No nodes registered. Use 'sdk-ops config add-node <ip>'")
				return nil
			}
			for _, n := range cfg.Nodes {
				host := n.Hostname
				if host == "" {
					host = "-"
				}
				fmt.Printf("  %s  user=%s  port=%d  mode=%s\n", n.IP, n.User, n.Port, n.Mode)
			}
			return nil
		},
	}

	var removeNodeCmd = &cobra.Command{
		Use:   "remove-node <ip>",
		Short: "Remove a node from the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			var updated []NodeConfig
			removed := false
			for _, n := range cfg.Nodes {
				if n.IP != args[0] {
					updated = append(updated, n)
				} else {
					removed = true
				}
			}
			if !removed {
				fmt.Printf("  Node %s not found\n", args[0])
				return nil
			}
			cfg.Nodes = updated
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("  Removed node %s\n", args[0])
			return nil
		},
	}

	var setCredsCmd = &cobra.Command{
		Use:   "set-credentials",
		Short: "Save provider credentials to ~/.sdk-ops/credentials.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := &providers.Credentials{
				CubePathAPIKey:    os.Getenv("CUBEPATH_API_KEY"),
				HetznerAPIToken:   os.Getenv("HETZNER_API_TOKEN"),
				DigitalOceanToken: os.Getenv("DIGITALOCEAN_TOKEN"),
				VultrAPIKey:       os.Getenv("VULTR_API_KEY"),
				AWSRegion:         os.Getenv("AWS_REGION"),
				AWSProfile:        os.Getenv("AWS_PROFILE"),
			}
			if err := providers.SaveCredentials(c); err != nil {
				return err
			}
			fmt.Printf("  Credentials saved to %s\n", providers.CredentialsPath())
			return nil
		},
	}

	cmd.AddCommand(initCmd)
	cmd.AddCommand(addCmd)
	cmd.AddCommand(listCmd)
	cmd.AddCommand(removeNodeCmd)
	cmd.AddCommand(setCredsCmd)

	return cmd
}
