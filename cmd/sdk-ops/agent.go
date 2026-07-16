package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	goss "golang.org/x/crypto/ssh"
)

func newAgentCmd() *cobra.Command {
	var user, key string
	var port int

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Check remote daemon status on nodes",
	}

	statusCmd := newAgentStatusCmd(&user, &key, &port)
	statusCmd.Flags().StringP("node", "n", "", "Target node IP")
	statusCmd.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
	statusCmd.Flags().StringVarP(&key, "key", "k", "", "SSH private key path")
	statusCmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.AddCommand(statusCmd)
	return cmd
}

func newAgentStatusCmd(user, key *string, port *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [--node ip]",
		Short: "Check remote daemon health on a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			useAgt, _ := cmd.Flags().GetBool("agt")
			if nodeIP == "" {
				cfg, err := loadConfig()
				if err != nil {
					return fmt.Errorf("--node is required (config: %w)", err)
				}
				if len(cfg.Nodes) > 0 {
					nodeIP = cfg.Nodes[0].IP
					*user = cfg.Nodes[0].User
					*key = cfg.Nodes[0].Key
					*port = cfg.Nodes[0].Port
				}
			}
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			if useAgt {
				ver, _, err := discoverAgt(nodeIP, *user, *key, *port)
				if err != nil {
					return fmt.Errorf("agt-swarm: %w", err)
				}
				healthJSON, _ := callAgtSSH(conn, "GET", "/system/health", "")
				fmt.Printf("  agt-swarm: [32mactive[0m (version: %s)\n", ver)
				fmt.Printf("  API: %s\n", healthJSON)
				return nil
			}

			health, _ := runSSH(conn, "curl -s --connect-timeout 5 http://127.0.0.1:44227/system/health 2>/dev/null || echo 'unreachable'")
			fmt.Printf("  daemon: %s\n", strings.TrimSpace(health))
			return nil
		},
	}
	cmd.Flags().Bool("agt", false, "Connect to agt-swarm daemon")
	return cmd
}

func discoverAgt(ip, user, key string, port int) (string, int, error) {
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return "", 0, fmt.Errorf("ssh: %w", err)
	}
	defer func() { _ = conn.Close() }()

	out, err := runSSH(conn, "systemctl is-active agt-server 2>/dev/null || echo inactive")
	if err != nil {
		return "", 0, fmt.Errorf("daemon check: %w", err)
	}
	status := strings.TrimSpace(out)
	if status != "active" {
		return "", 0, fmt.Errorf("agt-swarm not running on %s (status: %s)", ip, status)
	}

	out, err = runSSH(conn, "curl -sf --connect-timeout 5 --max-time 10 http://127.0.0.1:44227/system/version 2>/dev/null || echo '{}'")
	if err != nil {
		return "", 0, fmt.Errorf("daemon api: %w", err)
	}
	var vResp struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal([]byte(out), &vResp)
	return vResp.Version, 44227, nil
}

func callAgtSSH(conn *goss.Client, method, path, body string) (string, error) {
	curlCmd := fmt.Sprintf("curl -sf --connect-timeout 5 --max-time 10 -X %s http://127.0.0.1:44227%s", method, path)
	if body != "" {
		escaped := strings.ReplaceAll(body, "'", "'\\''")
		curlCmd = fmt.Sprintf("curl -sf --connect-timeout 5 --max-time 10 -X %s -H 'Content-Type: application/json' -d '%s' http://127.0.0.1:44227%s", method, escaped, path)
	}
	out, err := runSSH(conn, curlCmd)
	if err != nil {
		return "", fmt.Errorf("daemon api: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func runSSH(conn *goss.Client, cmd string) (string, error) {
	sess, err := conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	defer func() { if err := sess.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: session close error: %v\n", err) } }()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}


