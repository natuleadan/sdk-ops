package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	golang_ssh "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/deploy"
	"github.com/natuleadan/sdk-ops/docker"
	"github.com/natuleadan/sdk-ops/hooks"
	"github.com/natuleadan/sdk-ops/secrets"
	"github.com/natuleadan/sdk-ops/ssh"
	"github.com/natuleadan/sdk-ops/templates"
)

func RunCommand(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func newDeployCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy and manage services on nodes",
	}

	var deployCmd = &cobra.Command{
		Use:   "push <dir> [--node ip]",
		Short: "Upload and deploy a service to a node",
		Args:  cobra.MaximumNArgs(1),
		Long: `Upload a service directory to a node and run it.

Detects runtime (k3s > Docker > systemd) and deploys accordingly.
Supports docker-compose.yml, k8s YAML files, and run.sh scripts.

Use --all to deploy to all registered nodes in parallel.
Use --sops-key to auto-decrypt service.yaml with sops before deploying.

Examples:
  sdk-ops deploy push ./products-svc --node 188.xxx.xxx.xxx
  sdk-ops deploy push ./my-service --node 188.xxx.xxx.xxx --name custom-name
  sdk-ops deploy push --git https://github.com/user/service.git --node 188.xxx.xxx.xxx
  sdk-ops deploy push ./my-service --all
  sdk-ops deploy push ./my-service --sops-key age1...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gitURL, _ := cmd.Flags().GetString("git")

			sourceDir := ""
			if len(args) > 0 {
				sourceDir = args[0]
			}

			if gitURL != "" {
				tmpDir, err := os.MkdirTemp("", "sdk-deploy-*")
				if err != nil {
					return fmt.Errorf("create temp dir: %w", err)
				}
				defer os.RemoveAll(tmpDir)

				fmt.Printf("  → Cloning %s...\n", gitURL)
				cloneCmd := RunCommand("git", "clone", "--depth=1", gitURL, tmpDir)
				if err := cloneCmd.Run(); err != nil {
					return fmt.Errorf("git clone: %w", err)
				}

				entries, _ := os.ReadDir(tmpDir)
				if len(entries) == 1 && entries[0].IsDir() {
					sourceDir = filepath.Join(tmpDir, entries[0].Name())
				} else {
					sourceDir = tmpDir
				}
			}

			if sourceDir == "" {
				return fmt.Errorf("provide a directory or use --git <url>")
			}

			nodeIP, _ := cmd.Flags().GetString("node")
			name, _ := cmd.Flags().GetString("name")
			user, _ := cmd.Flags().GetString("user")
			key, _ := cmd.Flags().GetString("key")
			port, _ := cmd.Flags().GetInt("port")
			sopsKey, _ := cmd.Flags().GetString("sops-key")
			runAll, _ := cmd.Flags().GetBool("all")
			builderType, _ := cmd.Flags().GetString("builder")
			zeroDowntime, _ := cmd.Flags().GetBool("zero-downtime")
			runtimeMode, _ := cmd.Flags().GetString("runtime")
			deployDomain, _ := cmd.Flags().GetString("domain")

			if nodeIP != "" {
				if n := lookupNode(nodeIP); n != nil {
					if user == "" { user = n.User }
					if key == ""  { key = n.Key }
					if port == 0  { port = n.Port }
				}
			}

			cfg, cfgErr := loadConfig()
			if nodeIP == "" && !runAll {
				if cfgErr != nil {
					return fmt.Errorf("load config: %w", cfgErr)
				}
				if len(cfg.Nodes) == 0 {
					return fmt.Errorf("no nodes registered. Use --node <ip> or --all")
				}
				nodeIP = cfg.Nodes[0].IP
				user = cfg.Nodes[0].User
				key = cfg.Nodes[0].Key
				port = cfg.Nodes[0].Port
				fmt.Printf("  Using first registered node: %s\n", nodeIP)
			}

			if name == "" {
				name = strings.TrimSuffix(sourceDir, "/")
				if idx := strings.LastIndex(name, "/"); idx >= 0 {
					name = name[idx+1:]
				}
			}

			svcYamlPath := filepath.Join(sourceDir, "service.yaml")

			// Decrypt service.yaml if --sops-key
			if sopsKey != "" {
				if secrets.FileIsEncrypted(svcYamlPath) {
					fmt.Println("  → Decrypting service.yaml...")
					if err := secrets.DecryptFileInPlace(svcYamlPath); err != nil {
						return fmt.Errorf("decrypt: %w", err)
					}
					defer func() {
						fmt.Println("  → Re-encrypting service.yaml...")
						secrets.EncryptFile(svcYamlPath, sopsKey)
					}()
				}
			}

			// Build and push image using selected builder
			reg := deploy.DefaultRegistry()
			var imageRef string
			var buildErr error

			if builderType != "" {
				bt := deploy.BuilderType(builderType)
				startSpinner("Building (" + builderType + ")...")
				imageRef, buildErr = deploy.BuildImage(sourceDir, name, reg, bt)
				if buildErr != nil {
					stopSpinner("")
					fmt.Printf("  %s⚠ Build failed: %v%s\n", colorYellow, buildErr, colorReset)
				} else {
					stopSpinner("Image pushed to registry via " + builderType)
				}
			} else {
				detected := deploy.DetectBuilder(sourceDir)
				if detected == "" {
					fmt.Println("  → docker-compose detected, skipping build")
				} else {
					fmt.Printf("  → Detected builder: %s\n", detected)
					startSpinner("Building...")
					imageRef, buildErr = deploy.BuildImage(sourceDir, name, reg, detected)
					if buildErr != nil {
						stopSpinner("")
						fmt.Printf("  %s⚠ Build failed: %v%s\n", colorYellow, buildErr, colorReset)
					} else {
					stopSpinner("Image pushed to registry via " + string(detected))
					}
				}
			}

			if imageRef != "" {
				checkClient := newSSHClient(nodeIP, user, port, key)
				if checkConn, err := checkClient.Connect(); err == nil {
					dockerOut, _, _ := ssh.Run(checkConn, "command -v docker || echo 'no-docker'")
					if strings.TrimSpace(dockerOut) == "no-docker" {
						fmt.Println("  → Docker not found on node, installing...")
						docker.Install(checkConn)
					}
					if reg.Username != "" && reg.Password != "" {
						ssh.Run(checkConn, fmt.Sprintf("sudo docker login %s -u %s -p %s 2>/dev/null || true", reg.Server, reg.Username, reg.Password))
					}
					checkConn.Close()
				}
			}

			// Detect hasDB, appPort, and health config from service.yaml
			hasDB := false
			appPort := 8080
			healthURL := ""
			healthTimeout := 30
			if data, err := os.ReadFile(svcYamlPath); err == nil {
				hasDB = strings.Contains(string(data), "database:") && strings.Contains(string(data), "url:")
				inHealth := false
				for _, line := range strings.Split(string(data), "\n") {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "port:") {
						fmt.Sscanf(trimmed, "port: %d", &appPort)
					}
					if trimmed == "health:" {
						inHealth = true
						continue
					}
					if inHealth && (trimmed == "" || !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, ":")) {
						if strings.HasPrefix(trimmed, "path:") || strings.HasPrefix(trimmed, "url:") {
							var val string
							fmt.Sscanf(trimmed, "%s %s", &val, &val)
							if val != "" && !strings.HasPrefix(val, "#") && val != "path:" && val != "url:" {
								port := appPort
								if port == 0 {
									port = 8080
								}
								if strings.HasPrefix(val, "/") {
									healthURL = fmt.Sprintf("http://localhost:%d%s", port, val)
								} else {
									healthURL = val
								}
							}
						}
						if strings.HasPrefix(trimmed, "interval:") {
							fmt.Sscanf(trimmed, "interval: %d", &healthTimeout)
						}
					} else if inHealth && !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") && trimmed != "" {
						inHealth = false
					}
					// Also support flat health_url:/health_timeout: top-level keys
					if !inHealth {
						if strings.HasPrefix(trimmed, "health_url:") {
							fmt.Sscanf(trimmed, "health_url: %s", &healthURL)
						}
						if strings.HasPrefix(trimmed, "health_timeout:") {
							fmt.Sscanf(trimmed, "health_timeout: %d", &healthTimeout)
						}
					}
				}
			}
			if mainData, err := os.ReadFile(filepath.Join(sourceDir, "main.go")); err == nil {
				re := regexp.MustCompile(`Port:\s*(\d+)`)
				if matches := re.FindStringSubmatch(string(mainData)); len(matches) > 1 {
					if p, err := strconv.Atoi(matches[1]); err == nil && p > 0 {
						appPort = p
					}
				}
			}

			dockerComposePath := filepath.Join(sourceDir, "docker-compose.yml")
			if imageRef != "" {
				composeData := deploy.GenerateCompose(imageRef, name, appPort, hasDB)
				os.WriteFile(dockerComposePath, composeData, 0644)
				fmt.Printf("  → Generated docker-compose.yml (port %d, postgres: %v)\n", appPort, hasDB)

				if hasDB {
					if data, err := os.ReadFile(svcYamlPath); err == nil {
						updated := strings.ReplaceAll(string(data), "@localhost:", fmt.Sprintf("@%s-db:", name))
						updated = strings.ReplaceAll(updated, "@127.0.0.1:", fmt.Sprintf("@%s-db:", name))
						os.WriteFile(svcYamlPath, []byte(updated), 0644)
						fmt.Printf("  → Updated service.yaml to use %s-db hostname\n", name)
					}
				}
			}

			deployToOne := func(nip, nuser, nkey string, nport int) error {
				client := newSSHClient(nip, nuser, nport, nkey)
				conn, err := client.Connect()
				if err != nil {
					return fmt.Errorf("cannot connect to %s: %w\n  %sSuggestion: check that the server is reachable and port %d is open%s", nip, err, colorYellow, nport, colorReset)
				}
				defer conn.Close()

				// Run pre-deploy hooks
				hooks.Run(conn, "pre-deploy", map[string]string{
					"APP":  name,
					"NODE": nip,
				})

				uploadCfg := deploy.UploadConfig{
					ServiceName: name,
					SourceDir:   sourceDir,
					Exclude:     []string{".git", "node_modules", ".env", ".DS_Store", ".dockerignore"},
				}

				startSpinner("Uploading " + name + "...")
				result, err := deploy.UploadAndDeploy(conn, uploadCfg)
				if err != nil {
					stopSpinner("")
					return fmt.Errorf("upload: %w", err)
				}
				stopSpinner("Deployed v" + result.Version)

				if runtimeMode == "k3s" {
					domain := deployDomain
					if domain == "" {
						domain = name + ".local"
					}
					versionDir := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", name, result.Version)
					if err := deploy.DeployK3sFromCompose(conn, name, versionDir, imageRef); err != nil {
						return fmt.Errorf("k3s deploy: %w", err)
					}
					if deployDomain != "" {
						fmt.Printf("  → Access at http://%s/\n", deployDomain)
					}
				} else if runtimeMode == "swarm" {
					versionDir := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", name, result.Version)
					if err := deploy.DeploySwarm(conn, name, versionDir, imageRef); err != nil {
						return fmt.Errorf("swarm deploy: %w", err)
					}
				} else if runtimeMode == "bare" {
					versionDir := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", name, result.Version)
					if err := deploy.DeployBare(conn, name, versionDir); err != nil {
						return fmt.Errorf("bare deploy: %w", err)
					}
				} else if zeroDowntime {
					serviceDir := fmt.Sprintf("/opt/sdk-ops/services/%s", name)
					if err := deploy.DeployBlueGreen(conn, name, serviceDir, result.Version); err != nil {
						return fmt.Errorf("blue/green: %w", err)
					}
				} else {
					svcCfg := deploy.ServiceConfig{
						Name:          name,
						HealthURL:     healthURL,
						HealthTimeout: healthTimeout,
					}
					if err := deploy.RunService(conn, svcCfg); err != nil {
						return fmt.Errorf("deploy failed: %w", err)
					}

					if err := deploy.HealthCheck(conn, name, healthTimeout, healthURL); err != nil {
						fmt.Printf("\n  ⚠️  Health check failed on %s, rolling back...\n", nip)
						if rbErr := deploy.Rollback(conn, name); rbErr != nil {
							return fmt.Errorf("health: %v\nrollback also failed: %v", err, rbErr)
						}
						deploy.RunService(conn, svcCfg)
						return fmt.Errorf("health check failed on %s, rolled back", nip)
					}
				}

				// Run post-deploy hooks
				hooks.Run(conn, "post-deploy", map[string]string{
					"APP":     name,
					"NODE":    nip,
					"VERSION": result.Version,
				})

				// Detect actual runtime for state
				detectedRT := runtimeMode
				if detectedRT == "" {
					if out, _, _ := ssh.Run(conn, "command -v k3s && echo k3s || (command -v docker && echo docker) || echo systemd"); out != "" {
						detectedRT = strings.TrimSpace(out)
					}
				}
				// Record in state
				stateRecord("service", name, nip, result.Version, detectedRT, "ok", map[string]string{
					"port": fmt.Sprintf("%d", appPort),
				})

				fmt.Printf("\n%s✅ %s deployed on %s (v%s)%s\n", colorGreen, name, nip, result.Version, colorReset)
				return nil
			}

			if runAll {
				if cfgErr != nil {
					return fmt.Errorf("load config: %w", cfgErr)
				}
				if len(cfg.Nodes) == 0 {
					return fmt.Errorf("no nodes registered")
				}

				fmt.Printf("  → Deploying %s to %d nodes...\n", name, len(cfg.Nodes))
				var wg sync.WaitGroup
				errs := make(chan error, len(cfg.Nodes))

				for _, n := range cfg.Nodes {
					wg.Add(1)
					go func(node NodeConfig) {
						defer wg.Done()
						if err := deployToOne(node.IP, node.User, node.Key, node.Port); err != nil {
							errs <- err
						}
					}(n)
				}
				wg.Wait()
				close(errs)

				for e := range errs {
					fmt.Fprintf(os.Stderr, "error: %v\n", e)
				}
				return nil
			}

			return deployToOne(nodeIP, user, key, port)
		},
	}
	deployCmd.Flags().StringP("node", "n", "", "Target node IP (default: first registered node)")
	deployCmd.Flags().StringP("name", "N", "", "Service name (default: directory name)")
	deployCmd.Flags().StringP("user", "u", "root", "SSH user")
	deployCmd.Flags().StringP("key", "k", "", "SSH private key path")
	deployCmd.Flags().IntP("port", "p", 22, "SSH port")
	deployCmd.Flags().String("git", "", "Git repository URL (clones and deploys)")
	deployCmd.Flags().String("sops-key", "", "Auto-decrypt service.yaml with sops (age key)")
	deployCmd.Flags().Bool("all", false, "Deploy to all registered nodes in parallel")
	deployCmd.Flags().String("builder", "", "Build method: dockerfile, nixpacks, pack (default: auto-detect)")
	deployCmd.Flags().Bool("zero-downtime", false, "Blue/green deploy with zero downtime")
	deployCmd.Flags().String("runtime", "", "Runtime: docker (default), k3s, swarm, bare")
	deployCmd.Flags().String("domain", "", "Domain for k3s Ingress (required with --runtime k3s)")

	encryptCmd := &cobra.Command{
		Use:   "encrypt <file>",
		Short: "Encrypt a service.yaml with sops",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ageKey, _ := cmd.Flags().GetString("age-key")
			if ageKey == "" {
				return fmt.Errorf("--age-key is required")
			}
			return secrets.EncryptFile(args[0], ageKey)
		},
	}
	encryptCmd.Flags().String("age-key", "", "Age public key for encryption")

	decryptCmd := &cobra.Command{
		Use:   "decrypt <file>",
		Short: "Decrypt a sops-encrypted file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := secrets.DecryptFile(args[0])
			if err != nil {
				return err
			}
			fmt.Print(string(out))
			return nil
		},
	}

	var initCmd = &cobra.Command{
		Use:   "init <dir> --template <name>",
		Short: "Scaffold a new service from a template",
		Long: `Generate project structure from a template.

Templates:
  html            Static HTML site with Nginx
  node            Node.js Express app
  wordpress       WordPress with MySQL
  go              Go HTTP server (multi-stage build)
  nextjs          Next.js app (standalone output)
  python-fastapi  FastAPI async Python app (uvicorn)
  django          Django project (gunicorn + settings)

Run 'sdk-ops deploy init' without --template to list available templates.

Examples:
  sdk-ops deploy init ./my-site --template html
  sdk-ops deploy init ./my-blog --template wordpress
  sdk-ops deploy init ./my-api --template go --name products-svc
  sdk-ops deploy init ./my-app --template nextjs
  sdk-ops deploy init ./my-api --template python-fastapi
  sdk-ops deploy init ./my-site --template django --name myblog`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tmpl, _ := cmd.Flags().GetString("template")
			appName, _ := cmd.Flags().GetString("name")
			ciType, _ := cmd.Flags().GetString("ci")

			if tmpl == "" {
				templates.List()
				return nil
			}

			if err := templates.ValidateName(appName); err != nil {
				return err
			}

			dir := args[0]
			if err := templates.Scaffold(tmpl, dir); err != nil {
				return fmt.Errorf("scaffold: %w", err)
			}

			templates.InitServiceYAML(dir, appName)
			if ciType != "" {
				if err := templates.InitCICD(dir, ciType); err != nil {
					return fmt.Errorf("ci init: %w", err)
				}
				fmt.Printf("  → CI/CD: %s\n", ciType)
			}
			fmt.Printf("\n✅ %s scaffolded in %s\n", tmpl, dir)
			fmt.Printf("   Edit service.yaml, then:\n")
			fmt.Printf("   sdk-ops deploy push %s --node <ip>\n", dir)
			return nil
		},
	}
	initCmd.Flags().String("template", "", "Template name (html, node, wordpress, go)")
	initCmd.Flags().String("name", "app", "Service name")
	initCmd.Flags().String("ci", "", "Generate CI/CD config (github, gitlab)")

	cmd.AddCommand(initCmd)
	cmd.AddCommand(deployCmd)
	cmd.AddCommand(encryptCmd)
	cmd.AddCommand(decryptCmd)
	return cmd
}

func newServiceCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "service",
		Short: "Manage deployed services",
	}

	statusCmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show service status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runServiceStatus(nodeIP, name, user, key, port)
		},
	}

	logsCmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show service logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			tail, _ := cmd.Flags().GetInt("tail")
			follow, _ := cmd.Flags().GetBool("follow")
			return runServiceLogs(nodeIP, args[0], user, key, port, tail, follow)
		},
	}
	logsCmd.Flags().IntP("tail", "t", 50, "Number of lines to show")
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")

	restartCmd := &cobra.Command{
		Use:   "restart <name>",
		Short: "Restart a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			return runServiceRestart(nodeIP, args[0], user, key, port)
		},
	}

	rollbackCmd := &cobra.Command{
		Use:   "rollback <name>",
		Short: "Rollback to previous version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			return runServiceRollback(nodeIP, args[0], user, key, port)
		},
	}

	versionsCmd := &cobra.Command{
		Use:   "versions <name>",
		Short: "List deployed versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			return runServiceVersions(nodeIP, args[0], user, key, port)
		},
	}

	for _, sc := range []*cobra.Command{statusCmd, logsCmd, restartCmd, rollbackCmd, versionsCmd} {
		sc.Flags().StringP("node", "n", "", "Target node IP (default: first registered)")
		sc.Flags().StringP("user", "u", "root", "SSH user")
		sc.Flags().StringP("key", "k", "", "SSH private key path")
		sc.Flags().IntP("port", "p", 22, "SSH port")
	}

	rotateCmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate secrets (DB passwords, env vars)",
	}

	rotateDBCmd := &cobra.Command{
		Use:   "db <container-name>",
		Short: "Rotate database password (postgres, mysql, redis, mongodb)",
		Args:  cobra.ExactArgs(1),
		Long: `Generate a new random password for a database and update it.
Optionally specify --type and --new-pass.

Examples:
  sdk-ops service rotate db my-postgres --type postgres --node X
  sdk-ops service rotate db my-mysql --type mysql --new-pass supersecret --node X`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			dbType, _ := cmd.Flags().GetString("type")
			newPass, _ := cmd.Flags().GetString("new-pass")
			containerName := args[0]

			conn, err := connectNode(nodeIP, user, key, port)
			if err != nil {
				return err
			}
			defer conn.Close()

			var dt deploy.DBType
			switch dbType {
			case "postgres":
				dt = deploy.DBPostgres
			case "mysql":
				dt = deploy.DBMySQL
			case "redis":
				dt = deploy.DBRedis
			case "mongodb":
				dt = deploy.DBMongoDB
			default:
				return fmt.Errorf("unsupported db type: %s (use: postgres, mysql, redis, mongodb)", dbType)
			}

			startSpinner("Rotating " + dbType + " password...")
			result, err := deploy.RotateDBPassword(conn, dt, containerName, newPass)
			if err != nil {
				stopSpinner("")
				return fmt.Errorf("rotate db: %w", err)
			}
			stopSpinner("Password rotated")

			stateRecord("database", containerName, nodeIP, dbType, "docker", "ok", map[string]string{
				"password": result,
			})
			fmt.Printf("  Password: %s\n", result)
			return nil
		},
	}
	rotateDBCmd.Flags().String("type", "", "Database type (postgres, mysql, redis, mongodb)")
	rotateDBCmd.Flags().String("new-pass", "", "Explicit new password (auto-generated if empty)")

	rotateEnvCmd := &cobra.Command{
		Use:   "env <service-name>",
		Short: "Rotate an environment variable in a service",
		Args:  cobra.ExactArgs(1),
		Long: `Generate a new random value for an environment variable and restart.

Examples:
  sdk-ops service rotate env myservice --name API_KEY --node X
  sdk-ops service rotate env myservice --name DB_PASS --value secret456 --node X`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			envKey, _ := cmd.Flags().GetString("name")
			envValue, _ := cmd.Flags().GetString("value")
			serviceName := args[0]

			if envKey == "" {
				return fmt.Errorf("--name is required (env var name)")
			}

			conn, err := connectNode(nodeIP, user, key, port)
			if err != nil {
				return err
			}
			defer conn.Close()

			startSpinner("Rotating " + envKey + "...")
			if err := deploy.RotateServiceEnv(conn, serviceName, envKey, envValue); err != nil {
				stopSpinner("")
				return fmt.Errorf("rotate env: %w", err)
			}
			stopSpinner(envKey + " rotated")
			return nil
		},
	}
	rotateEnvCmd.Flags().String("name", "", "Environment variable name")
	rotateEnvCmd.Flags().String("value", "", "Explicit new value (auto-generated if empty)")

	for _, sc := range []*cobra.Command{rotateDBCmd, rotateEnvCmd} {
		sc.Flags().StringP("node", "n", "", "Target node IP (default: first registered)")
		sc.Flags().StringP("user", "u", "root", "SSH user")
		sc.Flags().StringP("key", "k", "", "SSH private key path")
		sc.Flags().IntP("port", "p", 22, "SSH port")
	}

	rotateCmd.AddCommand(rotateDBCmd)
	rotateCmd.AddCommand(rotateEnvCmd)

	cmd.AddCommand(statusCmd)
	cmd.AddCommand(logsCmd)
	cmd.AddCommand(restartCmd)
	cmd.AddCommand(rollbackCmd)
	cmd.AddCommand(versionsCmd)
	cmd.AddCommand(rotateCmd)

	return cmd
}

func getNodeFlags(cmd *cobra.Command) (ip, user, key string, port int) {
	ip, _ = cmd.Flags().GetString("node")
	user, _ = cmd.Flags().GetString("user")
	key, _ = cmd.Flags().GetString("key")
	port, _ = cmd.Flags().GetInt("port")

	if ip == "" {
		if cfg, err := loadConfig(); err == nil && len(cfg.Nodes) > 0 {
			ip = cfg.Nodes[0].IP
			if user == "" {
				user = cfg.Nodes[0].User
			}
			if key == "" {
				key = cfg.Nodes[0].Key
			}
			if port == 0 {
				port = cfg.Nodes[0].Port
			}
		}
	}
	if ip == "" {
		fmt.Fprintln(os.Stderr, "  error: no node specified. Use --node <ip> or register a node first.")
		os.Exit(1)
	}
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	return
}

func connectNode(ip, user, key string, port int) (*golang_ssh.Client, error) {
	client := newSSHClient(ip, user, port, key)
	conn, err := client.Connect()
	if err != nil {
		return nil, fmt.Errorf("ssh %s: %w", ip, err)
	}
	return conn, nil
}

func runServiceStatus(ip, name, user, key string, port int) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer conn.Close()

	if name != "" {
		out, err := deploy.ServiceStatus(conn, name)
		if err != nil {
			return err
		}
		fmt.Print(out)
	} else {
		services, err := deploy.ListServices(conn)
		if err != nil {
			return err
		}
		if len(services) == 0 {
			fmt.Printf("  No services deployed on %s\n", ip)
			return nil
		}
		fmt.Printf("  Services on %s:\n", ip)
		for _, s := range services {
			out, _ := deploy.ServiceStatus(conn, s)
			status := "unknown"
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "type:") {
					status = strings.TrimPrefix(line, "type:")
				}
			}
			fmt.Printf("    %-20s %s\n", s, status)
		}
	}
	return nil
}

func runServiceLogs(ip, name, user, key string, port int, tail int, follow bool) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer conn.Close()
	return deploy.ServiceLogs(conn, name, tail, follow)
}

func runServiceRestart(ip, name, user, key string, port int) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Printf("  Restarting %s on %s...\n", name, ip)
	return deploy.RunService(conn, deploy.ServiceConfig{Name: name})
}

func runServiceRollback(ip, name, user, key string, port int) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer conn.Close()

	return deploy.Rollback(conn, name)
}

func runServiceVersions(ip, name, user, key string, port int) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer conn.Close()

	versions, err := deploy.ListVersions(conn, name)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		fmt.Printf("  No versions found for %s on %s\n", name, ip)
		return nil
	}
	fmt.Printf("  Versions of %s on %s:\n", name, ip)
	for _, v := range versions {
		fmt.Printf("    %s\n", v)
	}
	return nil
}
