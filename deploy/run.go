package deploy

import (
	"fmt"
	"path/filepath"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type ServiceConfig struct {
	Name      string
	Type      string // docker, k3s, systemd
	Image     string // Docker image (for docker type)
	Port      int
	Replicas  int
	EnvFile   string
	HealthURL string
}

func RunService(client *goss.Client, cfg ServiceConfig) error {
	serviceDir := fmt.Sprintf("/opt/sdk-ops/services/%s", cfg.Name)
	currentDir := fmt.Sprintf("%s/current", serviceDir)

	runtime := detectRuntime(client, currentDir, cfg.Type)
	fmt.Printf("  → Runtime detected: %s\n", runtime)

	switch runtime {
	case "docker":
		return runDockerCompose(client, currentDir)
	case "k3s":
		return runKubectl(client, currentDir)
	default:
		return runSystemd(client, cfg.Name, currentDir)
	}
}

func detectRuntime(client *goss.Client, dir, preferred string) string {
	if preferred != "" {
		return preferred
	}
	// docker-compose.yml first (registry image)
	hasCompose, _, _ := ssh.Run(client,
		fmt.Sprintf(`test -f "%s/docker-compose.yml" -o -f "%s/docker-compose.yaml" && echo yes || echo no`, dir, dir))
	hasBinary, _, _ := ssh.Run(client,
		fmt.Sprintf(`ls "%s/sdk-ops-"*"-amd64" 2>/dev/null | head -1 || echo "no"`, dir))

	dockerOut, _, _ := ssh.Run(client, "command -v docker && echo yes || echo no")
	k3sOut, _, _ := ssh.Run(client, "command -v k3s && echo yes || echo no")

	// Prefer docker-compose over everything else
	if strings.Contains(hasCompose, "yes") && strings.Contains(dockerOut, "yes") {
		return "docker"
	}
	if strings.Contains(k3sOut, "yes") {
		return "k3s"
	}
	if strings.TrimSpace(hasBinary) != "no" {
		return "systemd"
	}
	if strings.Contains(dockerOut, "yes") {
		return "docker"
	}
	return "systemd"
}

func runDockerCompose(client *goss.Client, dir string) error {
	out, _, err := ssh.Run(client, fmt.Sprintf(
		`test -f "%s/docker-compose.yml" -o -f "%s/docker-compose.yaml" && echo yes || echo no`, dir, dir))
	if strings.TrimSpace(out) != "yes" {
		fmt.Println("  → No docker-compose file, checking for Dockerfile...")
		return buildAndRun(client, dir)
	}
	fmt.Println("  → Pulling image from registry and starting...")
	err = ssh.RunStream(client, fmt.Sprintf("cd %s && sudo docker compose pull && sudo docker compose up -d --remove-orphans", dir))
	if err != nil {
		return fmt.Errorf("docker compose: %w", err)
	}
	return nil
}

func buildAndRun(client *goss.Client, dir string) error {
	out, _, err := ssh.Run(client, fmt.Sprintf(
		`test -f "%s/Dockerfile" && echo yes || echo no`, dir))
	if strings.TrimSpace(out) != "yes" {
		fmt.Println("  → No Dockerfile found, skipping build")
		return nil
	}
	svcName := filepath.Base(dir)
	tag := fmt.Sprintf("%s:latest", svcName)
	fmt.Printf("  → Building Docker image: %s\n", tag)
	err = ssh.RunStream(client, fmt.Sprintf("cd %s && sudo docker build -t %s .", dir, tag))
	if err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

func runKubectl(client *goss.Client, dir string) error {
	kubeconfig := "/etc/rancher/k3s/k3s.yaml"
	out, _, _ := ssh.Run(client, fmt.Sprintf(`ls %s/*.yaml %s/*.yml 2>/dev/null | head -5`, dir, dir))
	if strings.TrimSpace(out) == "" {
		fmt.Println("  → No k8s YAML files, skipping")
		return nil
	}
	files := strings.Fields(out)
	for _, f := range files {
		base := filepath.Base(f)
		if base == "docker-compose.yml" || base == "docker-compose.yaml" || base == "service.yaml" {
			continue
		}
		fmt.Printf("  → Applying %s...\n", base)
		kOut, _, err := ssh.Run(client, fmt.Sprintf("KUBECONFIG=%s kubectl apply -f %s", kubeconfig, f))
		if err != nil {
			return fmt.Errorf("kubectl apply %s: %w\n%s", f, err, kOut)
		}
		fmt.Print(kOut)
	}
	return nil
}

func runSystemd(client *goss.Client, name, dir string) error {
	binaryOut, _, err := ssh.Run(client, fmt.Sprintf(
		`F=$(ls "%s/sdk-ops-%s-amd64" 2>/dev/null); if [ -n "$F" ]; then echo "$F"; else ls "%s/run.sh" "%s/%s" 2>/dev/null | head -1 || echo "none"; fi`,
		dir, name, dir, dir, name))
	binaryPath := strings.TrimSpace(binaryOut)
	execStart := binaryPath
	if strings.Contains(binaryPath, "run.sh") {
		execStart = fmt.Sprintf("%s/run.sh", dir)
	}

	if binaryPath == "none" || binaryPath == "" {
		fmt.Println("  → No binary or run.sh, skipping systemd setup")
		return nil
	}
	fmt.Printf("  → Binary found: %s\n", binaryPath)

	serviceContent := fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, name, dir, execStart)

	script := fmt.Sprintf(`sudo bash -c 'cat > /etc/systemd/system/%s.service << "EOF"
%s
EOF'
sudo systemctl daemon-reload
sudo systemctl enable %s 2>/dev/null
sudo systemctl restart %s
echo "systemd_ok"
`, name, serviceContent, name, name)

	cmdOut, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("systemd: %w\n%s", err, cmdOut)
	}
	fmt.Printf("  → Systemd service %s started\n", name)
	return nil
}

func ServiceStatus(client *goss.Client, name string) (string, error) {
	out, _, err := ssh.Run(client, fmt.Sprintf(`
CUR=$(readlink -f /opt/sdk-ops/services/%[1]s/current 2>/dev/null || echo "none")
echo "dir:$CUR"
if docker ps --format='{{.Names}}' 2>/dev/null | grep -q %[1]s; then echo "type:docker"
elif KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl get deploy -l app=%[1]s 2>/dev/null | grep -q .; then echo "type:k3s"
elif systemctl is-active %[1]s 2>/dev/null | grep -q active; then echo "type:systemd"
else echo "type:unknown"
fi`, name))
	return out, err
}

func ServiceLogs(client *goss.Client, name string, tail int, follow bool) error {
	if follow {
		cmd := fmt.Sprintf(`
if docker ps --format '{{.Names}}' 2>/dev/null | grep -q %[1]s; then
  docker logs -f --tail %[2]d $(docker ps --format '{{.Names}}' | grep %[1]s | head -1)
elif systemctl is-active %[1]s 2>/dev/null; then
  journalctl -u %[1]s -f -n %[2]d
else
  echo "No logs"
fi`, name, tail)
		return ssh.RunPTY(client, cmd)
	}
	out, _, err := ssh.Run(client, fmt.Sprintf(`
if docker ps --format '{{.Names}}' 2>/dev/null | grep -q %[1]s; then
  docker logs --tail %[2]d $(docker ps --format '{{.Names}}' | grep %[1]s | head -1)
elif systemctl is-active %[1]s 2>/dev/null; then
  journalctl -u %[1]s -n %[2]d --no-pager
else
  echo "No logs"
fi`, name, tail))
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func HealthCheck(client *goss.Client, name string, timeout int) error {
	fmt.Printf("  → Health check (%ds timeout)...\n", timeout)
	script := fmt.Sprintf(`
TIMEOUT=%d
INTERVAL=3
ELAPSED=0

for PORT in 18081 8080 3000; do
	timeout 2 bash -c "curl -s -o /dev/null -w '%%{http_code}' http://localhost:\$PORT/healthz 2>/dev/null | grep -q 200" 2>/dev/null && echo "healthy (port \$PORT/healthz)" && exit 0
	timeout 2 bash -c "curl -s -o /dev/null -w '%%{http_code}' http://localhost:\$PORT/health 2>/dev/null | grep -q 200" 2>/dev/null && echo "healthy (port \$PORT/health)" && exit 0
done

# Fallback: check containers
while [ $ELAPSED -lt $TIMEOUT ]; do
	CONTAINER=$(docker ps --format '{{.Names}}' 2>/dev/null | grep '%s' | head -1)
	if [ -n "$CONTAINER" ]; then
		STATUS=$(docker inspect "$CONTAINER" --format='{{.State.Status}}' 2>/dev/null)
		if [ "$STATUS" = "running" ]; then
			echo "healthy (docker: $CONTAINER)"
			exit 0
		fi
	fi
	KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl get pods -l app='%s' 2>/dev/null | awk 'NR>1 && $3 == "Running" {found=1} END {if(found) print "healthy (k8s)"; else exit 1}' && exit 0
	sleep $INTERVAL
	ELAPSED=$((ELAPSED + INTERVAL))
done

echo "unhealthy after ${TIMEOUT}s"
exit 1
`, timeout, name, name)

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("health check failed: %s", strings.TrimSpace(out))
	}
	fmt.Printf("  → %s", strings.TrimSpace(out))
	return nil
}

func ListServices(client *goss.Client) ([]string, error) {
	out, _, err := ssh.Run(client, `ls -1 /opt/sdk-ops/services/ 2>/dev/null || echo ""`)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
