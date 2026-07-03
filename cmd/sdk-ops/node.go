package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/natuleadan/sdk-ops/monitor"
	"github.com/natuleadan/sdk-ops/ssh"
)

func nodeFlagsWithConfig(ip, user, key string, port int) (string, string, int) {
	if node := lookupNode(ip); node != nil {
		if user == "" {
			user = node.User
		}
		if key == "" {
			key = node.Key
		}
		if port == 0 {
			port = node.Port
		}
	}
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	return user, key, port
}

func newNodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Monitor and manage nodes",
	}

	cmd.AddCommand(newNodeListCmd())
	cmd.AddCommand(newNodeInfoCmd())
	cmd.AddCommand(newNodeTopCmd())
	cmd.AddCommand(newNodeExecCmd())

	return cmd
}

func newNodeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if len(cfg.Nodes) == 0 {
				fmt.Println("  No nodes registered. Add one with:")
				fmt.Println("    sdk-ops config add-node <ip> --user root")
				return nil
			}
			fmt.Println("  Registered nodes:")
			for _, n := range cfg.Nodes {
				fmt.Printf("    %s  user=%s  port=%d", n.IP, n.User, n.Port)
				if n.Role != "" {
					fmt.Printf("  role=%s", n.Role)
				}
				if n.Arch != "" {
					fmt.Printf("  arch=%s", n.Arch)
				}
				if n.Mode != "" {
					fmt.Printf("  mode=%s", n.Mode)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

func newNodeInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <ip>",
		Short: "Show real-time node stats (htop-like)",
		Args:  cobra.ExactArgs(1),
		Long: `Show a real-time dashboard of node health including:
  CPU, memory, disk, network, uptime, and top processes.

Examples:
  sdk-ops node info 188.xxx.xxx.xxx
  sdk-ops node info 188.xxx.xxx.xxx --user ubuntu`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ip := args[0]
			user, _ := cmd.Flags().GetString("user")
			key, _ := cmd.Flags().GetString("key")
			port, _ := cmd.Flags().GetInt("port")
			user, key, port = nodeFlagsWithConfig(ip, user, key, port)

			client := newSSHClient(ip, user, port, key)

			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh connect: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "node: conn close error: %v\n", err) } }()

			stats, err := monitor.GetStats(conn)
			if err != nil {
				return err
			}
			runtime := monitor.GetRuntimeStatus(conn)
			procs, err := monitor.GetTopProcesses(conn, 8)
			if err != nil {
				return err
			}
			fmt.Print(monitor.FormatStats(stats, runtime, procs))
			return nil
		},
	}

	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

func newNodeTopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top <ip>",
		Short: "Open interactive htop on the remote node",
		Args:  cobra.ExactArgs(1),
		Long: `Open an interactive htop session on the remote node.
Requires htop to be installed (sdk-ops infra init installs it).

Examples:
  sdk-ops node top 188.xxx.xxx.xxx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ip := args[0]
			user, _ := cmd.Flags().GetString("user")
			key, _ := cmd.Flags().GetString("key")
			port, _ := cmd.Flags().GetInt("port")
			user, key, port = nodeFlagsWithConfig(ip, user, key, port)

			client := newSSHClient(ip, user, port, key)

			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh connect: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "node: conn close error: %v\n", err) } }()

			fmt.Printf("\n  Opening htop on %s...\n\n", ip)
			if _, _, err := ssh.Run(conn, "command -v htop >/dev/null 2>&1 || apt-get install -y -qq htop 2>&1"); err != nil { log.Printf("node: ssh run error: %v", err) }
			if err := monitor.RunInteractive(conn, "htop"); err != nil {
				return fmt.Errorf("htop failed: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	return cmd
}

func newNodeExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec [ip] -- <command>",
		Short: "Run a command on a remote node",
		Args:  cobra.MinimumNArgs(1),
		Long: `Run any command on a remote node and see the output.

Use --all to run on all registered nodes in parallel.
Use --servers to run only on server nodes.
Use --agents to run only on agent nodes.

Examples:
  sdk-ops node exec 188.xxx.xxx.xxx -- free -h
  sdk-ops node exec --all -- uptime
  sdk-ops node exec --servers -- sudo journalctl -u k3s -n 100
  sdk-ops node exec --agents -- df -h`,
		RunE: execRunE,
	}

	cmd.Flags().StringP("user", "u", "root", "SSH user")
	cmd.Flags().StringP("key", "k", "", "SSH private key path")
	cmd.Flags().IntP("port", "p", 22, "SSH port")
	cmd.Flags().Bool("all", false, "Run on all registered nodes in parallel")
	cmd.Flags().Bool("servers", false, "Run only on server nodes")
	cmd.Flags().Bool("agents", false, "Run only on agent nodes")
	return cmd
}

func execRunE(cmd *cobra.Command, args []string) error {
	runAll, _ := cmd.Flags().GetBool("all")
	runServers, _ := cmd.Flags().GetBool("servers")
	runAgents, _ := cmd.Flags().GetBool("agents")

	if runAll || runServers || runAgents {
		return execRunAll(cmd, args, runAll, runServers, runAgents)
	}

	ip := args[0]
	command := strings.Join(args[1:], " ")
	user, _ := cmd.Flags().GetString("user")
	key, _ := cmd.Flags().GetString("key")
	port, _ := cmd.Flags().GetInt("port")
	user, key, port = nodeFlagsWithConfig(ip, user, key, port)

	client := newSSHClient(ip, user, port, key)

	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("ssh connect: %w", err)
	}
	defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "node: conn close error: %v\n", err) } }()

	out, _, err := monitor.RunCommand(conn, command)
	if err != nil {
		return fmt.Errorf("command failed: %w\n%s", err, out)
	}
	fmt.Print(out)
	return nil
}

func filterExecNodes(cfg *RootConfig, runAll, runServers, runAgents bool) []NodeConfig {
	var nodes []NodeConfig
	for _, n := range cfg.Nodes {
		switch {
		case runAll:
			nodes = append(nodes, n)
		case runServers && runAgents:
			nodes = append(nodes, n)
		case runServers && n.Role == "server":
			nodes = append(nodes, n)
		case runAgents && n.Role == "agent":
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func execRunAll(cmd *cobra.Command, args []string, runAll, runServers, runAgents bool) error {
	command := strings.Join(args, " ")
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.Nodes) == 0 {
		return fmt.Errorf("no nodes registered")
	}

	nodes := filterExecNodes(cfg, runAll, runServers, runAgents)
	if len(nodes) == 0 {
		return fmt.Errorf("no matching nodes found")
	}

	return execOnNodes(cmd, nodes, command)
}

func execOnNodes(cmd *cobra.Command, nodes []NodeConfig, command string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(nodes))
	for _, n := range nodes {
		wg.Add(1)
		go func(node NodeConfig) {
			defer wg.Done()
			client := newSSHClient(node.IP, node.User, node.Port, node.Key)
			conn, err := client.Connect()
			if err != nil {
				errs <- fmt.Errorf("[%s] ssh: %w", node.IP, err)
				return
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "node: conn close error: %v\n", err) } }()

			out, _, err := monitor.RunCommand(conn, command)
			if err != nil {
				errs <- fmt.Errorf("[%s] %w\n%s", node.IP, err, out)
				return
			}
			fmt.Printf("--- %s ---\n%s\n", node.IP, out)
		}(n)
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", e); err != nil { log.Printf("node: write error: %v", err) }
	}
	return nil
}
