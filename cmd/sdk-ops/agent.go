package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	goss "golang.org/x/crypto/ssh"
)

func newAgentCmd() *cobra.Command {
	var user, key string
	var port int

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage the sdk-ops agent on nodes",
	}

	installCmd := newAgentInstallCmd(&user, &key, &port)
	statusCmd := newAgentStatusCmd(&user, &key, &port)
	logsCmd := newAgentLogsCmd(&user, &key, &port)
	updateCmd := newAgentUpdateCmd(&user, &key, &port)
	uninstallCmd := newAgentUninstallCmd(&user, &key, &port)
	scheduleCmd := newAgentScheduleCmd(&user, &key, &port)

	for _, sc := range []*cobra.Command{installCmd, statusCmd, logsCmd, uninstallCmd, updateCmd} {
		sc.Flags().StringP("node", "n", "", "Target node IP")
		sc.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
		sc.Flags().StringVarP(&key, "key", "k", "", "SSH private key path")
		sc.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	}
	logsCmd.Flags().IntP("tail", "t", 100, "Number of lines")
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	updateCmd.Flags().BoolP("force", "f", false, "Force rebuild even if up to date")
	installCmd.Flags().String("runtime", "", "Runtime: bare (default, systemd) or docker")
	uninstallCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	uninstallCmd.Flags().Bool("purge", false, "Also remove agent data (audit logs, metrics)")

	cmd.AddCommand(installCmd)
	cmd.AddCommand(statusCmd)
	cmd.AddCommand(logsCmd)
	cmd.AddCommand(uninstallCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(scheduleCmd)
	return cmd
}

func newAgentInstallCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "install --node <ip>",
		Short: "Build and deploy the agent as a systemd service (default) or Docker container",
		Long: `Build the agent, upload to the node, and run it.

Default (bare metal): installs as systemd service with full host access
  - CPU, RAM, disk, temperature, network, certs from the host
  - Docker socket for container monitoring

Docker container: use --runtime docker
  - Runs inside Docker with limited host access
  - Useful for testing or isolated environments

Configure notifications via env vars before installing:
  SDK_OPS_SLACK_WEBHOOK, SDK_OPS_DISCORD_WEBHOOK,
  SDK_OPS_TELEGRAM_TOKEN, SDK_OPS_TELEGRAM_CHAT_ID,
  SDK_OPS_SMTP_HOST, SDK_OPS_SMTP_USER, SDK_OPS_SMTP_PASS, etc.

Examples:
  sdk-ops agent install --node 1.2.3.4              # bare metal (systemd)
  sdk-ops agent install --node 1.2.3.4 --runtime docker  # Docker container`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			runtimeMode, _ := cmd.Flags().GetString("runtime")
			if nodeIP == "" {
				cfg, err := loadConfig()
				if err != nil {
					return fmt.Errorf("load config: %w. Use --node <ip>", err)
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

			conn, err := sshConnect(nodeIP, *user, *key, *port)
			if err != nil {
				return err
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			if runtimeMode == "docker" {
				return installAgentDocker(conn, nodeIP)
			}
			return installAgentBare(conn, nodeIP)
		},
	}
}

func newAgentStatusCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "status [--node ip]",
		Short: "Check agent health on a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			status, src := checkAgentStatus(conn)
			if status == "" {
				fmt.Printf("  ⚠️  Agent not found on %s\n", nodeIP)
				return nil
			}

			fmt.Printf("  Agent on %s: ", nodeIP)
			if status == "running" || status == "active" {
				fmt.Printf("\033[32m%s\033[0m (%s)\n", status, src)
			} else {
				fmt.Printf("%s (%s)\n", status, src)
			}

			health, _ := runSSH(conn, agentAPICmd("wget -qO- http://localhost:9000/health 2>/dev/null || echo '{\"status\":\"unreachable\"}'"))
			fmt.Printf("  API: %s\n", strings.TrimSpace(health))
			return nil
		},
	}
}

func newAgentLogsCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "logs [--node ip] [--tail N] [--follow]",
		Short: "Show agent logs (journalctl for systemd, docker logs for Docker)",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			tail, _ := cmd.Flags().GetInt("tail")
			follow, _ := cmd.Flags().GetBool("follow")

			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			fl := ""
			if follow {
				fl = "-f"
			}
			logCmd := fmt.Sprintf("journalctl -u sdk-ops-agent -n %d --no-pager %s 2>/dev/null || docker logs %s --tail %d sdk-ops-agent 2>&1", tail, fl, fl, tail)
			if err := streamSSH(conn, logCmd); err != nil {
				return fmt.Errorf("logs: %w", err)
			}
			return nil
		},
	}
}

func newAgentUpdateCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "update --node <ip> [--force]",
		Short: "Check and apply agent updates",
		Long: `Check the agent's current version against GitHub releases.
If a newer version is available, rebuild and restart the agent.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			force, _ := cmd.Flags().GetBool("force")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			vOut, vErr := runSSH(conn, agentAPICmd("wget -qO- http://localhost:9000/version")+" 2>/dev/null || echo '{\"current\":\"unknown\"}'")
			if vErr != nil {
				return fmt.Errorf("check version: %w", vErr)
			}
			fmt.Printf("  Agent version info: %s\n", strings.TrimSpace(vOut))

			if !force && strings.Contains(vOut, `"update_available":false`) {
				fmt.Println("  ✅ Agent is up to date")
				return nil
			}
			if force {
				fmt.Println("  → --force: rebuilding agent...")
			}
			return rebuildAgent(conn, nodeIP)
		},
	}
}

func newAgentUninstallCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall --node <ip> [--yes] [--purge]",
		Short: "Remove the agent from a node",
		Long: `Remove the agent (systemd service or Docker container).

By default, prompts for confirmation.
Use --yes to skip confirmation.
Use --purge to also remove agent data (audit logs, metrics, schedules).

Examples:
  sdk-ops agent uninstall --node 1.2.3.4             # prompts
  sdk-ops agent uninstall --node 1.2.3.4 --yes       # skip prompt
  sdk-ops agent uninstall --node 1.2.3.4 --yes --purge  # remove everything`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			yes, _ := cmd.Flags().GetBool("yes")
			purge, _ := cmd.Flags().GetBool("purge")

			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			if !yes {
				fmt.Printf("  This will remove the agent from %s.\n", nodeIP)
				if purge {
					fmt.Printf("  WARNING: --purge will delete ALL agent data (audit logs, metrics, schedules).\n")
				} else {
					fmt.Printf("  Agent data (audit logs, metrics) will be kept at /opt/sdk-ops/agent-data.\n")
				}
				fmt.Printf("  Are you sure? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				response, _ := reader.ReadString('\n')
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("  Cancelled.")
					return nil
				}
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			uninstallAgentFromConn(conn, nodeIP, purge)
			return nil
		},
	}
}

func uninstallAgentFromConn(conn *goss.Client, nodeIP string, purge bool) {
	if _, err := runSSH(conn, "systemctl stop sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "systemctl disable sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "rm -f /etc/systemd/system/sdk-ops-agent.service"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "systemctl daemon-reload 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "docker rm -f sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "docker rmi sdk-ops-agent:latest 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "rm -rf /opt/sdk-ops/agent"); err != nil {
		log.Printf("ssh: %v", err)
	}

	if purge {
		if _, err := runSSH(conn, "rm -rf /opt/sdk-ops/agent-data"); err != nil {
			log.Printf("ssh: %v", err)
		}
		if _, err := runSSH(conn, "docker volume rm sdk-ops-agent-data 2>/dev/null || true"); err != nil {
			log.Printf("ssh: %v", err)
		}
		fmt.Printf("  ✅ Agent removed from %s (data purged)\n", nodeIP)
	} else {
		fmt.Printf("  ✅ Agent removed from %s (data kept at /opt/sdk-ops/agent-data)\n", nodeIP)
	}
}

func newAgentScheduleCmd(user, key *string, port *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage scheduled tasks via the agent",
	}

	addCmd := newAgentScheduleAddCmd(user, key, port)
	listCmd := newAgentScheduleListCmd(user, key, port)
	rmCmd := newAgentScheduleRemoveCmd(user, key, port)

	for _, sc := range []*cobra.Command{addCmd, listCmd, rmCmd} {
		sc.Flags().StringP("node", "n", "", "Target node IP")
		sc.Flags().StringVarP(user, "user", "u", "root", "SSH user")
		sc.Flags().StringVarP(key, "key", "k", "", "SSH private key path")
		sc.Flags().IntVarP(port, "port", "p", 22, "SSH port")
	}

	addCmd.Flags().String("cron", "", "Cron expression (e.g., '0 3 * * *')")
	addCmd.Flags().String("task", "", "Task type: backup-services, backup-database, docker-cleanup, shell")
	addCmd.Flags().String("config", "", "Task config (e.g., container name for backup-database)")
	addCmd.Flags().String("notify", "failure", "Notify on: failure, always, never")

	cmd.AddCommand(addCmd)
	cmd.AddCommand(listCmd)
	cmd.AddCommand(rmCmd)
	return cmd
}

func newAgentScheduleAddCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> --cron <expr> --task <type> [--notify failure|always|never] [--node ip]",
		Short: "Add a scheduled task to the agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cronExpr, _ := cmd.Flags().GetString("cron")
			taskType, _ := cmd.Flags().GetString("task")
			taskConfig, _ := cmd.Flags().GetString("config")
			notifyOn, _ := cmd.Flags().GetString("notify")
			nodeIP, _ := cmd.Flags().GetString("node")

			if nodeIP == "" || cronExpr == "" || taskType == "" {
				return fmt.Errorf("--node, --cron, and --task are required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			payload := fmt.Sprintf(`{"name":"%s","cron_expr":"%s","task_type":"%s","task_config":"%s","notify_on":"%s"}`,
				name, cronExpr, taskType, taskConfig, notifyOn)
			cmdStr := agentAPICmd(fmt.Sprintf("wget -qO- --post-data='%s' --header='Content-Type: application/json' http://localhost:9000/schedules", payload))
			_, rErr := runSSH(conn, cmdStr)
			if rErr != nil {
				return fmt.Errorf("agent unreachable on %s: %v", nodeIP, rErr)
			}
			fmt.Printf("  ✅ Schedule %q added to agent\n", name)
			return nil
		},
	}
}

func newAgentScheduleListCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "list [--node ip]",
		Short: "List scheduled tasks from the agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			out, lErr := runSSH(conn, agentAPICmd("wget -qO- http://localhost:9000/schedules")+" || echo 'agent-unreachable'")
			if lErr != nil || strings.Contains(out, "agent-unreachable") {
				return fmt.Errorf("agent unreachable on %s", nodeIP)
			}
			fmt.Println(strings.TrimSpace(out))
			return nil
		},
	}
}

func newAgentScheduleRemoveCmd(user, key *string, port *int) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id> [--node ip]",
		Short: "Remove a scheduled task from the agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, *user, *port, *key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer func() { if err := conn.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: conn close error: %v\n", err) } }()

			cmdStr := agentAPICmd(fmt.Sprintf("wget -qO- 'http://localhost:9000/schedules/remove?id=%s'", id))
			resp, rErr := runSSH(conn, cmdStr)
			if rErr != nil {
				return fmt.Errorf("agent unreachable on %s: %v", nodeIP, rErr)
			}
			fmt.Printf("  ✅ Schedule %s removed\n%s", id, resp)
			return nil
		},
	}
}

func agentAPICmd(inner string) string {
	return fmt.Sprintf("if systemctl -q is-active sdk-ops-agent 2>/dev/null; then %s; elif docker inspect sdk-ops-agent --format='{{.State.Status}}' 2>/dev/null | grep -q running; then docker exec sdk-ops-agent %s; else echo 'agent-unreachable'; fi", inner, inner)
}

func checkAgentStatus(conn *goss.Client) (status, src string) {
	out, err := runSSH(conn, "systemctl is-active sdk-ops-agent 2>/dev/null || echo inactive")
	if err != nil {
		log.Printf("agent: check status error: %v", err)
	}
	status = strings.TrimSpace(out)
	if status == "active" {
		return "active", "systemd"
	}
	out, err = runSSH(conn, `docker inspect sdk-ops-agent --format='{{.State.Status}}' 2>/dev/null || echo "not-found"`)
	if err != nil {
		log.Printf("agent: docker inspect error: %v", err)
	}
	status = strings.TrimSpace(out)
	if status != "" && status != "not-found" {
		return status, "docker"
	}
	return "", ""
}

func uploadAgentBinary(conn *goss.Client, binaryPath string) error {
	binData, err := os.ReadFile(filepath.Clean(binaryPath))
	if err != nil {
		return err
	}
	if _, err := runSSH(conn, "mkdir -p /opt/sdk-ops/agent"); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	sess, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer func() { if err := sess.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: session close error: %v\n", err) } }()

	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	defer func() { if err := r.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: pipe close error: %v\n", err) } }()

	sess.Stdin = r
	errCh := make(chan error, 1)
	go func() {
		out, err := sess.CombinedOutput("tar xzf - -C /opt/sdk-ops/agent && chmod 755 /opt/sdk-ops/agent/sdk-ops-agent")
		if err != nil {
			errCh <- fmt.Errorf("remote: %w\n%s", err, string(out))
		} else {
			errCh <- nil
		}
	}()

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "sdk-ops-agent", Size: int64(len(binData)), Mode: 0755}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(binData); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}
	if err := tw.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: tar close error: %v\n", err) }
	if err := gw.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: gzip close error: %v\n", err) }
	if err := w.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: pipe write close error: %v\n", err) }

	return <-errCh
}

func buildAgentBinary() (string, error) {
	fmt.Println("  → Building agent binary (linux/amd64)...")
	buildCmd := exec.CommandContext(context.Background(), "go", "build",
		"-a",
		"-ldflags=-s -w -X main.version="+version,
		"-o", "/tmp/sdk-ops-agent-linux",
		"./agent/")
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build: %w\n%s", err, string(out))
	}
	info, _ := os.Stat("/tmp/sdk-ops-agent-linux")
	if info != nil {
		fmt.Printf("  → Binary size: %.1f MB\n", float64(info.Size())/1024/1024)
	}
	return "/tmp/sdk-ops-agent-linux", nil
}

func installAgentBare(conn *goss.Client, nodeIP string) error {
	if _, err := runSSH(conn, "docker rm -f sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}

	binaryPath, err := buildAgentBinary()
	if err != nil {
		return err
	}
	defer func() { if err := os.Remove(binaryPath); err != nil { fmt.Fprintf(os.Stderr, "agent: remove binary error: %v\n", err) } }()

	fmt.Println("  → Uploading agent binary...")
	if err := uploadAgentBinary(conn, binaryPath); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	fmt.Println("  → Installing systemd service...")
	dataDir := "/opt/sdk-ops/agent-data"
	if _, err := runSSH(conn, fmt.Sprintf("mkdir -p %s", dataDir)); err != nil {
		log.Printf("ssh: %v", err)
	}

	notifyEnvs := collectNotifyEnvVars()
	var envLines strings.Builder
	for _, e := range notifyEnvs {
		fmt.Fprintf(&envLines, "Environment=%s\n", e)
	}

	unitContent := fmt.Sprintf(`[Unit]
Description=sdk-ops-agent
After=network.target docker.service
Wants=docker.socket

[Service]
Type=simple
ExecStart=/opt/sdk-ops/agent/sdk-ops-agent
WorkingDirectory=/opt/sdk-ops/agent
Environment=SDK_OPS_AGENT_DB=%s/sdk-ops-agent.db
%sRestart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, dataDir, envLines.String())

	installScript := fmt.Sprintf(`
cat > /etc/systemd/system/sdk-ops-agent.service << 'SERVICEEOF'
%s
SERVICEEOF
systemctl daemon-reload
systemctl enable --now sdk-ops-agent 2>&1
systemctl restart sdk-ops-agent 2>&1
echo "done"`, unitContent)

	if out, err := runSSH(conn, installScript); err != nil {
		return fmt.Errorf("systemd install: %w\n%s", err, out)
	}

	time.Sleep(3 * time.Second)
	out, _ := runSSH(conn, "systemctl is-active sdk-ops-agent 2>/dev/null || echo inactive")
	status := strings.TrimSpace(out)

	if status == "active" {
		logOut, _ := runSSH(conn, "journalctl -u sdk-ops-agent -n 5 --no-pager 2>/dev/null")
		fmt.Printf("\n✅ Agent deployed on %s (status: active, systemd)\n", nodeIP)
		fmt.Printf("   Logs:\n%s\n", strings.TrimSpace(logOut))
	} else {
		logOut, _ := runSSH(conn, "journalctl -u sdk-ops-agent -n 20 --no-pager 2>/dev/null")
		return fmt.Errorf("agent status: %s\nlogs:\n%s", status, logOut)
	}
	return nil
}

func buildAgentDockerUpload(conn *goss.Client, binaryPath string) error {
	fmt.Println("  → Uploading agent...")
	dfData := []byte(`FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata docker-cli
COPY sdk-ops-agent /usr/local/bin/
EXPOSE 9000
VOLUME /data
ENTRYPOINT ["sdk-ops-agent"]`)

	binData, _ := os.ReadFile(filepath.Clean(binaryPath))
	if _, err := runSSH(conn, "mkdir -p /opt/sdk-ops/agent"); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	sess, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer func() { if err := sess.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: session close error: %v\n", err) } }()
	r, w, _ := os.Pipe()
	sess.Stdin = r
	errCh := make(chan error, 1)
	go func() {
		out, err := sess.CombinedOutput("tar xzf - -C /opt/sdk-ops/agent && chmod 755 /opt/sdk-ops/agent/sdk-ops-agent")
		if err != nil {
			errCh <- fmt.Errorf("remote: %w\n%s", err, string(out))
		} else {
			errCh <- nil
		}
	}()
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "sdk-ops-agent", Size: int64(len(binData)), Mode: 0755}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(binData); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "Dockerfile", Size: int64(len(dfData)), Mode: 0644}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(dfData); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}
	if err := tw.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: tar close error: %v\n", err) }
	if err := gw.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: gzip close error: %v\n", err) }
	if err := w.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: pipe write close error: %v\n", err) }
	if err := <-errCh; err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	return nil
}

func startAgentDockerContainer(conn *goss.Client) error {
	fmt.Println("  → Building Docker image...")
	if out, err := runSSH(conn, "cd /opt/sdk-ops/agent && docker build -t sdk-ops-agent:latest . 2>&1"); err != nil {
		return fmt.Errorf("docker build: %w\n%s", err, out)
	}

	fmt.Println("  → Starting container...")
	if _, err := runSSH(conn, "docker rm -f sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	volumes := "-v /var/run/docker.sock:/var/run/docker.sock -v /opt/sdk-ops:/opt/sdk-ops:ro"
	notifyEnvs := collectNotifyEnvVars()
	var envFlags strings.Builder
	for _, e := range notifyEnvs {
		fmt.Fprintf(&envFlags, " -e '%s'", e)
	}
	runCmd := fmt.Sprintf("docker run -d --name sdk-ops-agent --restart unless-stopped %s %s -v sdk-ops-agent-data:/data sdk-ops-agent:latest", volumes, envFlags.String())
	if out, err := runSSH(conn, runCmd); err != nil {
		return fmt.Errorf("docker run: %w\n%s", err, out)
	}

	time.Sleep(3 * time.Second)
	return nil
}

func installAgentDocker(conn *goss.Client, nodeIP string) error {
	if _, err := runSSH(conn, "systemctl stop sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "systemctl disable sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "rm -f /etc/systemd/system/sdk-ops-agent.service"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "systemctl daemon-reload 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}

	binaryPath, err := buildAgentBinary()
	if err != nil {
		return err
	}
	defer func() { if err := os.Remove(binaryPath); err != nil { fmt.Fprintf(os.Stderr, "agent: remove binary error: %v\n", err) } }()

	if err := buildAgentDockerUpload(conn, binaryPath); err != nil {
		return err
	}

	if err := startAgentDockerContainer(conn); err != nil {
		return err
	}

	status, _ := runSSH(conn, `docker inspect sdk-ops-agent --format='{{.State.Status}}'`)
	if strings.TrimSpace(status) == "running" {
		fmt.Printf("\n✅ Agent deployed on %s (status: running, Docker)\n", nodeIP)
	} else {
		logOut, _ := runSSH(conn, "docker logs --tail 20 sdk-ops-agent 2>&1")
		return fmt.Errorf("agent status: %s\n%s", status, logOut)
	}
	return nil
}

func rebuildAgent(conn *goss.Client, nodeIP string) error {
	binaryPath, err := buildAgentBinary()
	if err != nil {
		return err
	}
	defer func() { if err := os.Remove(binaryPath); err != nil { fmt.Fprintf(os.Stderr, "agent: remove binary error: %v\n", err) } }()

	fmt.Println("  → Uploading agent binary...")
	if err := uploadAgentBinary(conn, binaryPath); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	out, _ := runSSH(conn, "systemctl restart sdk-ops-agent 2>&1 && echo 'systemd-ok' || echo 'systemd-fail'")
	if strings.Contains(out, "systemd-ok") {
		time.Sleep(2 * time.Second)
		status, _ := runSSH(conn, "systemctl is-active sdk-ops-agent 2>/dev/null")
		if strings.TrimSpace(status) == "active" {
			fmt.Printf("✅ Agent updated on %s (systemd)\n", nodeIP)
			return nil
		}
		return fmt.Errorf("agent not active after restart: %s", strings.TrimSpace(status))
	}

	fmt.Println("  → Rebuilding Docker image...")
	if _, err := runSSH(conn, "cd /opt/sdk-ops/agent && docker build -t sdk-ops-agent:latest . 2>&1 || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	if _, err := runSSH(conn, "docker rm -f sdk-ops-agent 2>/dev/null || true"); err != nil {
		log.Printf("ssh: %v", err)
	}
	volumes := "-v /var/run/docker.sock:/var/run/docker.sock -v /opt/sdk-ops:/opt/sdk-ops:ro"
	var envFlags strings.Builder
	for _, e := range collectNotifyEnvVars() {
		fmt.Fprintf(&envFlags, " -e '%s'", e)
	}
	if _, err := runSSH(conn, fmt.Sprintf("docker run -d --name sdk-ops-agent --restart unless-stopped %s %s -v sdk-ops-agent-data:/data sdk-ops-agent:latest", volumes, envFlags.String())); err != nil {
		log.Printf("ssh: %v", err)
	}
	time.Sleep(3 * time.Second)
	fmt.Printf("✅ Agent updated on %s (Docker)\n", nodeIP)
	return nil
}

func collectNotifyEnvVars() []string {
	var envs []string
	for _, pair := range os.Environ() {
		if strings.HasPrefix(pair, "SDK_OPS_SLACK_") ||
			strings.HasPrefix(pair, "SDK_OPS_DISCORD_") ||
			strings.HasPrefix(pair, "SDK_OPS_TELEGRAM_") ||
			strings.HasPrefix(pair, "SDK_OPS_SMTP_") ||
			strings.HasPrefix(pair, "SDK_OPS_WEBHOOK_") {
			envs = append(envs, pair)
		}
	}
	return envs
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

func streamSSH(conn *goss.Client, cmd string) error {
	sess, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer func() { if err := sess.Close(); err != nil { fmt.Fprintf(os.Stderr, "agent: session close error: %v\n", err) } }()
	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr
	return sess.Run(cmd)
}

func sshConnect(nodeIP, user, key string, port int) (*goss.Client, error) {
	return newSSHClient(nodeIP, user, port, key).Connect()
}
