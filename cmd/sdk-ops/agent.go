package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
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

	installCmd := &cobra.Command{
		Use:   "install --node <ip>",
		Short: "Build and deploy the agent container to a node",
		Long: `Build the agent, upload to the node, build Docker image, and run.

The agent collects system metrics every 60s, runs scheduled tasks,
sends notifications, and maintains an audit log.

Configure notifications via env vars before installing:
  SDK_OPS_SLACK_WEBHOOK, SDK_OPS_DISCORD_WEBHOOK,
  SDK_OPS_TELEGRAM_TOKEN, SDK_OPS_TELEGRAM_CHAT_ID,
  SDK_OPS_SMTP_HOST, SDK_OPS_SMTP_USER, SDK_OPS_SMTP_PASS, etc.

Examples:
  sdk-ops agent install --node 1.2.3.4
  SDK_OPS_SLACK_WEBHOOK=url sdk-ops agent install --node 1.2.3.4`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				cfg, err := loadConfig()
				if err != nil {
					return fmt.Errorf("load config: %w. Use --node <ip>", err)
				}
				if len(cfg.Nodes) > 0 {
					nodeIP = cfg.Nodes[0].IP
					user = cfg.Nodes[0].User
					key = cfg.Nodes[0].Key
					port = cfg.Nodes[0].Port
				}
			}
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			// Step 1: Cross-compile agent binary for linux/amd64
			fmt.Println("  → Building agent binary (linux/amd64)...")
			buildCmd := exec.Command("go", "build",
				"-ldflags=-s -w -X main.version="+version,
				"-o", "/tmp/sdk-ops-agent-linux",
				"./agent/")
			buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
			if out, err := buildCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("build agent: %w\n%s", err, string(out))
			}
			defer os.Remove("/tmp/sdk-ops-agent-linux")

			// Step 2: Create Dockerfile locally
			dockerfile := `FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata docker-cli
COPY sdk-ops-agent /usr/local/bin/
EXPOSE 9000
VOLUME /data
ENTRYPOINT ["sdk-ops-agent"]`

			// Step 3: Upload via tar.gz with binary + Dockerfile
			fmt.Println("  → Uploading agent...")
			if err := uploadAgentFiles(conn, "/tmp/sdk-ops-agent-linux", dockerfile); err != nil {
				return fmt.Errorf("upload: %w", err)
			}
			fmt.Println("  → Files uploaded")

			// Step 4: Build Docker image on VPS
			fmt.Println("  → Building Docker image (this may take a minute)...")
			buildImgCmd := "cd /opt/sdk-ops/agent && docker build -t sdk-ops-agent:latest . 2>&1"
			if out, _, err := runSSH(conn, buildImgCmd); err != nil {
				return fmt.Errorf("docker build: %w\n%s", err, out)
			}
			fmt.Println("  → Docker image built")

			// Step 5: Stop existing agent if running
			runSSH(conn, "docker rm -f sdk-ops-agent 2>/dev/null || true")

			// Step 6: Run agent container with notification env vars
			fmt.Println("  → Starting agent container...")
			volumes := "-v /var/run/docker.sock:/var/run/docker.sock -v /opt/sdk-ops:/opt/sdk-ops:ro"
			notifyEnvs := collectNotifyEnvVars()
			envFlags := ""
			for _, e := range notifyEnvs {
				envFlags += fmt.Sprintf(" -e '%s'", e)
			}

			runCmd := fmt.Sprintf(
				"docker run -d --name sdk-ops-agent --restart unless-stopped %s %s -v sdk-ops-agent-data:/data sdk-ops-agent:latest",
				volumes, envFlags)
			if out, _, err := runSSH(conn, runCmd); err != nil {
				return fmt.Errorf("docker run: %w\n%s", err, out)
			}

			// Wait a moment and verify
			time.Sleep(3 * time.Second)
			checkCmd := "docker inspect sdk-ops-agent --format='{{.State.Status}}'"
			status, _, _ := runSSH(conn, checkCmd)
			status = strings.TrimSpace(status)

			if status == "running" {
				logsOut, _, _ := runSSH(conn, "docker logs --tail 5 sdk-ops-agent 2>&1")
				fmt.Printf("\n✅ Agent deployed on %s (status: running)\n", nodeIP)
				fmt.Printf("   API: http://localhost:9000/health (internal)\n")
				fmt.Printf("   Logs:\n%s\n", strings.TrimSpace(logsOut))
			} else {
				logsOut, _, _ := runSSH(conn, "docker logs --tail 20 sdk-ops-agent 2>&1")
				return fmt.Errorf("agent status: %s\nlogs:\n%s", status, logsOut)
			}
			return nil
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status [--node ip]",
		Short: "Check agent health on a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			out, _, _ := runSSH(conn, `docker inspect sdk-ops-agent --format='{{.State.Status}}' 2>/dev/null || echo "not-found"`)
			status := strings.TrimSpace(out)

			if status == "not-found" || status == "" {
				fmt.Printf("  ⚠️  Agent not found on %s\n", nodeIP)
				return nil
			}

			fmt.Printf("  Agent on %s: ", nodeIP)
			if status == "running" {
				fmt.Printf("\033[32mrunning\033[0m\n")
			} else {
				fmt.Printf("%s\n", status)
			}

			// Check health via docker exec (API is internal to container)
			health, _, _ := runSSH(conn, `docker exec sdk-ops-agent wget -qO- http://localhost:9000/health 2>/dev/null || echo '{"status":"unreachable"}'`)
			fmt.Printf("  API: %s\n", strings.TrimSpace(health))
			return nil
		},
	}

	logsCmd := &cobra.Command{
		Use:   "logs [--node ip] [--tail N] [--follow]",
		Short: "Show agent logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			tail, _ := cmd.Flags().GetInt("tail")
			follow, _ := cmd.Flags().GetBool("follow")

			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			fl := ""
			if follow {
				fl = "-f"
			}
			logCmd := fmt.Sprintf("docker logs %s --tail %d sdk-ops-agent 2>&1", fl, tail)
			if err := streamSSH(conn, logCmd); err != nil {
				return fmt.Errorf("logs: %w", err)
			}
			return nil
		},
	}

	uninstallCmd := &cobra.Command{
		Use:   "uninstall --node <ip>",
		Short: "Remove the agent from a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, _ := cmd.Flags().GetString("node")
			if nodeIP == "" {
				return fmt.Errorf("--node is required")
			}

			client := newSSHClient(nodeIP, user, port, key)
			conn, err := client.Connect()
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}
			defer conn.Close()

			runSSH(conn, "docker rm -f sdk-ops-agent 2>/dev/null || true")
			runSSH(conn, "docker rmi sdk-ops-agent:latest 2>/dev/null || true")
			runSSH(conn, "rm -rf /opt/sdk-ops/agent")
			fmt.Printf("  ✅ Agent removed from %s\n", nodeIP)
			return nil
		},
	}

	for _, sc := range []*cobra.Command{installCmd, statusCmd, logsCmd, uninstallCmd} {
		sc.Flags().StringP("node", "n", "", "Target node IP")
		sc.Flags().StringVarP(&user, "user", "u", "root", "SSH user")
		sc.Flags().StringVarP(&key, "key", "k", "", "SSH private key path")
		sc.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	}
	logsCmd.Flags().IntP("tail", "t", 100, "Number of lines")
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")

	cmd.AddCommand(installCmd)
	cmd.AddCommand(statusCmd)
	cmd.AddCommand(logsCmd)
	cmd.AddCommand(uninstallCmd)
	return cmd
}

func uploadAgentFiles(conn *goss.Client, binaryPath, dockerfile string) error {
	binData, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}

	// Ensure target directory exists
	if _, _, err := runSSH(conn, "mkdir -p /opt/sdk-ops/agent"); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Upload binary via SSH session stdin pipe
	sess, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	// Create a tar.gz piped through SSH
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	defer r.Close()

	sess.Stdin = r

	// Start remote command
	errCh := make(chan error, 1)
	go func() {
		out, err := sess.CombinedOutput("tar xzf - -C /opt/sdk-ops/agent && chmod 755 /opt/sdk-ops/agent/sdk-ops-agent")
		if err != nil {
			errCh <- fmt.Errorf("remote: %w\n%s", err, string(out))
		} else {
			errCh <- nil
		}
	}()

	// Write tar.gz to pipe
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	// Add binary
	if err := tw.WriteHeader(&tar.Header{
		Name: "sdk-ops-agent", Size: int64(len(binData)), Mode: 0755,
	}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(binData); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}

	// Add Dockerfile
	dfData := []byte(dockerfile)
	if err := tw.WriteHeader(&tar.Header{
		Name: "Dockerfile", Size: int64(len(dfData)), Mode: 0644,
	}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(dfData); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}

	tw.Close()
	gw.Close()
	w.Close()

	return <-errCh
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

func runSSH(conn *goss.Client, cmd string) (string, string, error) {
	sess, err := conn.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(cmd)
	return string(out), "", err
}

func streamSSH(conn *goss.Client, cmd string) error {
	sess, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr
	return sess.Run(cmd)
}
