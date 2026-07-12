package main

import (
	"context"
	"fmt"
	"log"
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

func newDeployCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy and manage services on nodes",
	}

	cmd.AddCommand(newDeployPushCmd())
	cmd.AddCommand(newDeployEncryptCmd())
	cmd.AddCommand(newDeployDecryptCmd())
	cmd.AddCommand(newDeployInitCmd())
	return cmd
}

func newDeployPushCmd() *cobra.Command {
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
		RunE: runDeployPush,
	}
	deployCmd.Flags().StringP("node", "n", "", "Target node IP (default: first registered node)")
	deployCmd.Flags().StringP("name", "N", "", "Service name (default: directory name)")
	deployCmd.Flags().StringP("user", "u", "root", "SSH user")
	deployCmd.Flags().StringP("key", "k", "", "SSH private key path")
	deployCmd.Flags().IntP("port", "p", 22, "SSH port")
	deployCmd.Flags().String("git", "", "Git repository URL (clones and deploys)")
	deployCmd.Flags().String("branch", "", "Git branch to clone (requires --git)")
	deployCmd.Flags().String("ssh-key", "", "SSH key for git clone (requires --git)")
	deployCmd.Flags().String("sops-key", "", "Auto-decrypt service.yaml with sops (age key)")
	deployCmd.Flags().Bool("all", false, "Deploy to all registered nodes in parallel")
	deployCmd.Flags().String("builder", "", "Build method: dockerfile, nixpacks, pack (default: auto-detect)")
	deployCmd.Flags().Bool("zero-downtime", false, "Blue/green deploy with zero downtime")
	deployCmd.Flags().String("runtime", "", "Runtime: docker (default), k3s, swarm, bare")
	deployCmd.Flags().String("domain", "", "Domain for k3s Ingress (required with --runtime k3s)")
	return deployCmd
}

type deployPushFlags struct {
	nodeIP       string
	name         string
	user         string
	key          string
	port         int
	sopsKey      string
	runAll       bool
	builderType  string
	zeroDowntime bool
	runtimeMode  string
	deployDomain string
}

func parseDeployPushFlags(cmd *cobra.Command) deployPushFlags {
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
	return deployPushFlags{
		nodeIP: nodeIP, name: name, user: user, key: key, port: port,
		sopsKey: sopsKey, runAll: runAll, builderType: builderType,
		zeroDowntime: zeroDowntime, runtimeMode: runtimeMode, deployDomain: deployDomain,
	}
}

func resolveDeployNode(flags *deployPushFlags) {
	if flags.nodeIP == "" {
		return
	}
	n := lookupNode(flags.nodeIP)
	if n == nil {
		return
	}
	if flags.user == "" {
		flags.user = n.User
	}
	if flags.key == "" {
		flags.key = n.Key
	}
	if flags.port == 0 {
		flags.port = n.Port
	}
}

func runDeployPush(cmd *cobra.Command, args []string) error {
	sourceDir, cleanup, err := resolveSourceDir(cmd, args)
	if err != nil {
		return err
	}
	defer cleanup()
	if sourceDir == "" {
		return fmt.Errorf("provide a directory or use --git <url>")
	}
	sourceDir = sanitizeSourceDir(sourceDir)

	flags := parseDeployPushFlags(cmd)
	resolveDeployNode(&flags)
	cfg, cfgErr := loadConfig()
	if flags.nodeIP == "" && !flags.runAll {
		if cfgErr != nil {
			return fmt.Errorf("load config: %w", cfgErr)
		}
		if len(cfg.Nodes) == 0 {
			return fmt.Errorf("no nodes registered. Use --node <ip> or --all")
		}
		flags.nodeIP = cfg.Nodes[0].IP
		flags.user = cfg.Nodes[0].User
		flags.key = cfg.Nodes[0].Key
		flags.port = cfg.Nodes[0].Port
		fmt.Printf("  Using first registered node: %s\n", flags.nodeIP)
	}

	flags.name = resolveServiceName(flags.name, sourceDir)

	svcYamlPath := filepath.Join(sourceDir, "service.yaml")

	reencrypt, err := decryptSecretsIfNeeded(svcYamlPath, flags.sopsKey)
	if err != nil {
		return err
	}
	defer reencrypt()

	reg := deploy.DefaultRegistry()
	imageRef := buildImage(sourceDir, flags.name, flags.builderType, reg)

	if imageRef != "" {
		ensureDockerOnNode(flags.nodeIP, flags.user, flags.key, flags.port, reg)
	}

	appPort, healthURL, healthTimeout, hasDB := parseServiceConfig(svcYamlPath, sourceDir)

	if imageRef != "" {
		if err := generateComposeAndServiceYaml(imageRef, flags.name, appPort, hasDB, sourceDir, svcYamlPath); err != nil {
			return err
		}
	}

	if flags.runAll {
		if cfgErr != nil {
			return fmt.Errorf("load config: %w", cfgErr)
		}
		if len(cfg.Nodes) == 0 {
			return fmt.Errorf("no nodes registered")
		}
		deployToAllNodes(flags, cfg.Nodes, sourceDir, appPort, healthURL, healthTimeout, imageRef)
		return nil
	}

	return deployToOne(flags.nodeIP, flags.user, flags.key, flags.port, flags.name, sourceDir, flags.runtimeMode, flags.deployDomain, healthURL, healthTimeout, imageRef, flags.zeroDowntime, appPort)
}

func resolveSourceDir(cmd *cobra.Command, args []string) (sourceDir string, cleanup func(), err error) {
	cleanup = func() {}

	gitURL, _ := cmd.Flags().GetString("git")
	gitBranch, _ := cmd.Flags().GetString("branch")
	gitSSHKey, _ := cmd.Flags().GetString("ssh-key")

	if len(args) > 0 {
		sourceDir = args[0]
	}

	if gitURL == "" {
		return sourceDir, cleanup, nil
	}

	tmpDir, tmpErr := os.MkdirTemp("", "sdk-deploy-*")
	if tmpErr != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", tmpErr)
	}
	cleanup = func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Printf("deploy: remove tmp dir error: %v", err)
		}
	}

	cloneArgs := []string{"clone", "--depth=1"}
	if gitBranch != "" {
		cloneArgs = append(cloneArgs, "--branch", gitBranch)
	}
	cloneArgs = append(cloneArgs, gitURL, tmpDir)

	cloneCmd := exec.CommandContext(context.Background(), "git", cloneArgs...)
	if gitSSHKey != "" {
		cloneCmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o StrictHostKeyChecking=no", gitSSHKey))
	}

	fmt.Printf("  → Cloning %s", gitURL)
	if gitBranch != "" {
		fmt.Printf(" (branch: %s)", gitBranch)
	}
	fmt.Println("...")
	if err := cloneCmd.Run(); err != nil {
		return "", cleanup, fmt.Errorf("git clone: %w", err)
	}

	entries, _ := os.ReadDir(tmpDir)
	if len(entries) == 1 && entries[0].IsDir() {
		sourceDir = filepath.Join(tmpDir, entries[0].Name())
	} else {
		sourceDir = tmpDir
	}
	return sourceDir, cleanup, nil
}

func resolveServiceName(name, sourceDir string) string {
	if name != "" {
		return name
	}
	name = strings.TrimSuffix(sourceDir, "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func decryptSecretsIfNeeded(svcYamlPath, sopsKey string) (reencrypt func(), err error) {
	reencrypt = func() {}
	if sopsKey == "" {
		return
	}
	if !secrets.FileIsEncrypted(svcYamlPath) {
		return
	}
	fmt.Println("  → Decrypting service.yaml...")
	if err := secrets.DecryptFileInPlace(svcYamlPath); err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	reencrypt = func() {
		fmt.Println("  → Re-encrypting service.yaml...")
		if err := secrets.EncryptFile(svcYamlPath, sopsKey); err != nil {
			log.Printf("deploy: re-encrypt error: %v", err)
		}
	}
	return
}

func buildImage(sourceDir, name, builderType string, reg deploy.RegistryConfig) string {
	if builderType != "" {
		bt := deploy.BuilderType(builderType)
		startSpinner("Building (" + builderType + ")...")
		imageRef, buildErr := deploy.BuildImage(sourceDir, name, reg, bt)
		if buildErr != nil {
			stopSpinner("")
			fmt.Printf("  %s⚠ Build failed: %v%s\n", colorYellow, buildErr, colorReset)
			return ""
		}
		stopSpinner("Image pushed to registry via " + builderType)
		return imageRef
	}

	detected := deploy.DetectBuilder(sourceDir)
	if detected == "" {
		fmt.Println("  → docker-compose detected, skipping build")
		return ""
	}
	fmt.Printf("  → Detected builder: %s\n", detected)
	startSpinner("Building...")
	imageRef, buildErr := deploy.BuildImage(sourceDir, name, reg, detected)
	if buildErr != nil {
		stopSpinner("")
		fmt.Printf("  %s⚠ Build failed: %v%s\n", colorYellow, buildErr, colorReset)
		return ""
	}
	stopSpinner("Image pushed to registry via " + string(detected))
	return imageRef
}

func ensureDockerOnNode(nodeIP, user, key string, port int, reg deploy.RegistryConfig) {
	checkClient := newSSHClient(nodeIP, user, port, key)
	checkConn, err := checkClient.Connect()
	if err != nil {
		return
	}
	defer func() {
		if err := checkConn.Close(); err != nil {
			log.Printf("deploy: check conn close error: %v", err)
		}
	}()

	dockerOut, _, _ := ssh.Run(checkConn, "command -v docker || echo 'no-docker'")
	if strings.TrimSpace(dockerOut) == "no-docker" {
		fmt.Println("  → Docker not found on node, installing...")
		if err := docker.Install(checkConn); err != nil {
			log.Printf("deploy: docker install error: %v", err)
		}
	}
	if reg.Username != "" && reg.Password != "" {
		cmd := fmt.Sprintf("sudo docker login %s -u %s --password-stdin 2>/dev/null || true", reg.Server, reg.Username)
		if _, _, err := ssh.RunWithStdin(checkConn, cmd, reg.Password+"\n"); err != nil {
			log.Printf("deploy: docker login failed (check registry credentials)")
		}
	}
}

func parseServiceConfig(svcYamlPath, sourceDir string) (appPort int, healthURL string, healthTimeout int, hasDB bool) {
	appPort = 8080
	healthTimeout = 30

	hasDB, data, err := readServiceYamlData(svcYamlPath)
	if err == nil && data != "" {
		appPort, healthURL, healthTimeout = parseServiceYamlLines(data, appPort)
	}
	if p := parsePortFromMain(sourceDir); p > 0 {
		appPort = p
	}
	return
}

func readServiceYamlData(svcYamlPath string) (hasDB bool, rawData string, err error) {
	data, err := os.ReadFile(filepath.Clean(svcYamlPath))
	if err != nil {
		return false, "", err
	}
	hasDB = strings.Contains(string(data), "database:") && strings.Contains(string(data), "url:")
	return hasDB, string(data), nil
}

func parseServiceYamlLines(data string, defaultPort int) (appPort int, healthURL string, healthTimeout int) {
	appPort = defaultPort
	healthTimeout = 30
	inHealth := false
	for line := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "port:") {
			parseYamlPortField(trimmed, &appPort)
		}
		if trimmed == "health:" {
			inHealth = true
			continue
		}
		if inHealth {
			inHealth = handleHealthSectionLine(trimmed, &healthURL, &healthTimeout, appPort)
		}
		if !inHealth {
			parseYamlFlatHealthField(trimmed, &healthURL, &healthTimeout)
		}
	}
	return
}

func parseYamlPortField(trimmed string, appPort *int) {
	if _, err := fmt.Sscanf(trimmed, "port: %d", appPort); err != nil {
		log.Printf("deploy: parse port error: %v", err)
	}
}

func handleHealthSectionLine(trimmed string, healthURL *string, healthTimeout *int, appPort int) bool {
	if trimmed == "" || !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, ":") {
		if strings.HasPrefix(trimmed, "path:") || strings.HasPrefix(trimmed, "url:") {
			val := parseHealthURLField(trimmed)
			if url := buildHealthURL(val, appPort); url != "" {
				*healthURL = url
			}
		}
		if strings.HasPrefix(trimmed, "interval:") {
			if _, err := fmt.Sscanf(trimmed, "interval: %d", healthTimeout); err != nil {
				log.Printf("deploy: parse interval error: %v", err)
			}
		}
		return true
	}
	if !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") && trimmed != "" {
		return false
	}
	return true
}

func parseHealthURLField(trimmed string) string {
	var val string
	if _, err := fmt.Sscanf(trimmed, "%s %s", &val, &val); err != nil {
		log.Printf("deploy: parse url error: %v", err)
	}
	return val
}

func buildHealthURL(val string, appPort int) string {
	if val == "" || strings.HasPrefix(val, "#") || val == "path:" || val == "url:" {
		return ""
	}
	port := appPort
	if port == 0 {
		port = 8080
	}
	if strings.HasPrefix(val, "/") {
		return fmt.Sprintf("http://localhost:%d%s", port, val)
	}
	return val
}

func parseYamlFlatHealthField(trimmed string, healthURL *string, healthTimeout *int) {
	if strings.HasPrefix(trimmed, "health_url:") {
		if _, err := fmt.Sscanf(trimmed, "health_url: %s", healthURL); err != nil {
			log.Printf("deploy: parse health url error: %v", err)
		}
	}
	if strings.HasPrefix(trimmed, "health_timeout:") {
		if _, err := fmt.Sscanf(trimmed, "health_timeout: %d", healthTimeout); err != nil {
			log.Printf("deploy: parse health timeout error: %v", err)
		}
	}
}

func parsePortFromMain(sourceDir string) int {
	mainData, err := os.ReadFile(filepath.Clean(filepath.Join(sourceDir, "main.go")))
	if err != nil {
		return 0
	}
	re := regexp.MustCompile(`Port:\s*(\d+)`)
	matches := re.FindStringSubmatch(string(mainData))
	if len(matches) > 1 {
		if p, err := strconv.Atoi(matches[1]); err == nil && p > 0 {
			return p
		}
	}
	return 0
}

func generateComposeAndServiceYaml(imageRef, name string, appPort int, hasDB bool, sourceDir, svcYamlPath string) error {
	composeData := deploy.GenerateCompose(imageRef, name, appPort, hasDB)
	if err := os.WriteFile(filepath.Join(sourceDir, "docker-compose.yml"), composeData, 0600); err != nil {
		return fmt.Errorf("write compose: %w", err)
	}
	fmt.Printf("  → Generated docker-compose.yml (port %d, postgres: %v)\n", appPort, hasDB)

	if hasDB {
		if data, err := os.ReadFile(filepath.Clean(svcYamlPath)); err == nil {
			updated := strings.ReplaceAll(string(data), "@localhost:", fmt.Sprintf("@%s-db:", name))
			updated = strings.ReplaceAll(updated, "@127.0.0.1:", fmt.Sprintf("@%s-db:", name))
			if err := writeFileSafe(filepath.Join(sourceDir, "service.yaml"), []byte(updated), 0600); err != nil {
				log.Printf("deploy: update service yaml error: %v", err)
			}
			fmt.Printf("  → Updated service.yaml to use %s-db hostname\n", name)
		}
	}
	return nil
}

func deployToOne(nip, nuser, nkey string, nport int, name, sourceDir, runtimeMode, deployDomain, healthURL string, healthTimeout int, imageRef string, zeroDowntime bool, appPort int) error {
	client := newSSHClient(nip, nuser, nport, nkey)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %w\n  %sSuggestion: check that the server is reachable and port %d is open%s", nip, err, colorYellow, nport, colorReset)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
		}
	}()

	if err := hooks.Run(conn, "pre-deploy", map[string]string{
		"APP":  name,
		"NODE": nip,
	}); err != nil {
		log.Printf("deploy: hooks error: %v", err)
	}

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

	if err := deployRuntimeOnNode(conn, name, result, runtimeMode, deployDomain, healthURL, healthTimeout, imageRef, zeroDowntime, nip); err != nil {
		return err
	}

	if err := hooks.Run(conn, "post-deploy", map[string]string{
		"APP":     name,
		"NODE":    nip,
		"VERSION": result.Version,
	}); err != nil {
		log.Printf("deploy: hooks error: %v", err)
	}

	detectedRT := runtimeMode
	if detectedRT == "" {
		if out, _, _ := ssh.Run(conn, "command -v k3s && echo k3s || (command -v docker && echo docker) || echo systemd"); out != "" {
			detectedRT = strings.TrimSpace(out)
		}
	}
	stateRecord("service", name, nip, result.Version, detectedRT, "ok", map[string]string{
		"port": fmt.Sprintf("%d", appPort),
	})

	fmt.Printf("\n%s✅ %s deployed on %s (v%s)%s\n", colorGreen, name, nip, result.Version, colorReset)
	return nil
}

func deployRuntimeOnNode(conn *golang_ssh.Client, name string, result *deploy.DeployResult, runtimeMode, deployDomain, healthURL string, healthTimeout int, imageRef string, zeroDowntime bool, nip string) error {
	switch runtimeMode {
	case "k3s":
		versionDir := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", name, result.Version)
		if err := deploy.DeployK3sFromCompose(conn, name, versionDir, imageRef); err != nil {
			return fmt.Errorf("k3s deploy: %w", err)
		}
		if deployDomain != "" {
			fmt.Printf("  → Access at http://%s/\n", deployDomain)
		}
	case "swarm":
		versionDir := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", name, result.Version)
		if err := deploy.DeploySwarm(conn, name, versionDir, imageRef); err != nil {
			return fmt.Errorf("swarm deploy: %w", err)
		}
	case "bare":
		versionDir := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", name, result.Version)
		if err := deploy.DeployBare(conn, name, versionDir); err != nil {
			return fmt.Errorf("bare deploy: %w", err)
		}
	default:
		return defaultDeploy(conn, name, healthURL, healthTimeout, result, zeroDowntime, nip)
	}
	return nil
}

func defaultDeploy(conn *golang_ssh.Client, name, healthURL string, healthTimeout int, result *deploy.DeployResult, zeroDowntime bool, nip string) error {
	if zeroDowntime {
		serviceDir := fmt.Sprintf("/opt/sdk-ops/services/%s", name)
		if err := deploy.DeployBlueGreen(conn, name, serviceDir, result.Version); err != nil {
			return fmt.Errorf("blue/green: %w", err)
		}
		return nil
	}
	return runServiceWithHealthCheck(conn, name, healthURL, healthTimeout, nip)
}

func runServiceWithHealthCheck(conn *golang_ssh.Client, name, healthURL string, healthTimeout int, nip string) error {
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
		if rbErr := deploy.Rollback(conn, name, ""); rbErr != nil {
			return fmt.Errorf("health: %v\nrollback also failed: %v", err, rbErr)
		}
		if err := deploy.RunService(conn, svcCfg); err != nil {
			log.Printf("deploy: run service error: %v", err)
		}
		return fmt.Errorf("health check failed on %s, rolled back", nip)
	}
	return nil
}

func deployToAllNodes(flags deployPushFlags, nodes []NodeConfig, sourceDir string, appPort int, healthURL string, healthTimeout int, imageRef string) {
	fmt.Printf("  → Deploying %s to %d nodes...\n", flags.name, len(nodes))
	var wg sync.WaitGroup
	errs := make(chan error, len(nodes))

	for _, n := range nodes {
		wg.Add(1)
		go func(node NodeConfig) {
			defer wg.Done()
			if err := deployToOne(node.IP, node.User, node.Key, node.Port, flags.name, sourceDir, flags.runtimeMode, flags.deployDomain, healthURL, healthTimeout, imageRef, flags.zeroDowntime, appPort); err != nil {
				errs <- err
			}
		}(n)
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		fmt.Fprintf(os.Stderr, "error: %v\n", e)
	}
}

func newDeployEncryptCmd() *cobra.Command {
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
	return encryptCmd
}

func newDeployDecryptCmd() *cobra.Command {
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
	return decryptCmd
}

func newDeployInitCmd() *cobra.Command {
	var initCmd = &cobra.Command{
		Use:   "init <dir> --template <name>",
		Short: "Scaffold a new service from a template",
		Long: `Generate project structure from a template.

Application templates:
  html            Static HTML site with Nginx
  node            Node.js Express app  
  wordpress       WordPress with MySQL
  go              Go HTTP server (multi-stage build)
  nextjs          Next.js app (standalone output)
  python-fastapi  FastAPI async Python app (uvicorn)
  django          Django project (gunicorn + settings)

Infrastructure templates (Docker Compose services):
  pg-full-bm     PostgreSQL 18 + PgDog + SSL + pgbackrest

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
			}

			if err := templates.ValidateName(appName); err != nil {
				return err
			}

			dir := args[0]
			if err := templates.Scaffold(tmpl, dir); err != nil {
				return fmt.Errorf("scaffold: %w", err)
			}

			if err := templates.InitServiceYAML(dir, appName); err != nil {
				log.Printf("deploy: init yaml error: %v", err)
			}
			if ciType != "" {
				if err := templates.InitCICD(dir, ciType); err != nil {
					return fmt.Errorf("ci init: %w", err)
				}
				fmt.Printf("  → CI/CD: %s\n", ciType)
			}

			tested, _ := cmd.Flags().GetBool("tested")
			if tested {
				fmt.Printf("  → Running integration test...\n")
				if err := templates.RunTest(tmpl, dir); err != nil {
					return fmt.Errorf("test failed: %w", err)
				}
				fmt.Printf("  ✅ Integration test passed\n")
			}

			fmt.Printf("\n✅ %s scaffolded in %s\n", tmpl, dir)
			fmt.Printf("   Edit service.yaml, then:\n")
			fmt.Printf("   sdk-ops deploy push %s --node <ip>\n", dir)
			return nil
		},
	}
	initCmd.Flags().String("template", "", "Template name (run 'deploy init --help' for list)")
	initCmd.Flags().Bool("tested", false, "Run integration test after scaffold (requires deployed services)")
	initCmd.Flags().String("name", "app", "Service name")
	initCmd.Flags().String("ci", "", "Generate CI/CD config (github, gitlab)")
	return initCmd
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
		Short: "Rollback to previous (or --version N)",
		Args:  cobra.ExactArgs(1),
		Long: `Rollback a service to a previous version.

Without flags: rollback to the previous deployed version (symlink swap).
With --version: rollback to a specific version number.
With --diff: show changes between versions without rolling back.

Examples:
  sdk-ops service rollback myservice
  sdk-ops service rollback myservice --version v3
  sdk-ops service rollback myservice --diff
  sdk-ops service rollback myservice --version v3 --diff`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeIP, user, key, port := getNodeFlags(cmd)
			version, _ := cmd.Flags().GetString("version")
			showDiff, _ := cmd.Flags().GetBool("diff")
			return runServiceRollback(nodeIP, args[0], user, key, port, version, showDiff)
		},
	}
	rollbackCmd.Flags().String("version", "", "Target version to rollback to (e.g. v3)")
	rollbackCmd.Flags().Bool("diff", false, "Show diff between versions without rolling back")

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
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
				}
			}()

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
			defer func() {
				if err := conn.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
				}
			}()

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
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
		}
	}()

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
			for line := range strings.SplitSeq(out, "\n") {
				line = strings.TrimSpace(line)
				if after, ok := strings.CutPrefix(line, "type:"); ok {
					status = after
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
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
		}
	}()
	return deploy.ServiceLogs(conn, name, tail, follow)
}

func runServiceRestart(ip, name, user, key string, port int) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
		}
	}()

	fmt.Printf("  Restarting %s on %s...\n", name, ip)
	return deploy.RunService(conn, deploy.ServiceConfig{Name: name})
}

func runServiceRollback(ip, name, user, key string, port int, version string, showDiff bool) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
		}
	}()

	if showDiff {
		// Show diff between current and target version
		versions, err := deploy.ListVersions(conn, name)
		if err != nil {
			return fmt.Errorf("list versions: %w", err)
		}
		if len(versions) < 2 {
			fmt.Printf("  No previous versions to compare for %s\n", name)
			return nil
		}

		current := versions[len(versions)-1]
		target := version
		if target == "" {
			if len(versions) < 2 {
				return fmt.Errorf("no previous version")
			}
			target = versions[len(versions)-2]
		}

		diff, err := deploy.DiffVersions(conn, name, current, target)
		if err != nil {
			return fmt.Errorf("diff: %w", err)
		}
		if diff == "" {
			fmt.Printf("  No differences between %s and %s\n", current, target)
			return nil
		}
		fmt.Printf("  Changes (%s → %s):\n", current, target)
		for line := range strings.SplitSeq(strings.TrimSpace(diff), "\n") {
			fmt.Printf("    %s\n", line)
		}
		return nil
	}

	if version != "" {
		return deploy.Rollback(conn, name, version)
	}
	return deploy.Rollback(conn, name, "")
}

func runServiceVersions(ip, name, user, key string, port int) error {
	conn, err := connectNode(ip, user, key, port)
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "deploy: conn close error: %v\n", err)
		}
	}()

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

func sanitizeSourceDir(dir string) string {
	if !filepath.IsAbs(dir) {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return ""
		}
		dir = absDir
	}
	return filepath.Clean(dir)
}

func writeFileSafe(path string, data []byte, perm os.FileMode) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return fmt.Errorf("open root: %w", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "root close: %v\n", err)
		}
	}()
	relPath := strings.TrimPrefix(path, "/")
	f, err := root.OpenFile(relPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "file close: %v\n", err)
		}
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	return nil
}
