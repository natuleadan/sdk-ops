package deploy

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func DeployBlueGreen(client *goss.Client, name, serviceDir, version string) error {
	fmt.Println("  → Blue/green deploy starting...")

	currentPort := getCurrentPort(client, name)
	greenPort := currentPort + 1
	if greenPort < 1024 {
		greenPort = 8080
	}

	fmt.Printf("  → Current port: %d, New port: %d\n", currentPort, greenPort)

	versionDir := fmt.Sprintf("%s/%s", serviceDir, version)

	if err := startGreenContainer(client, versionDir, greenPort); err != nil {
		return err
	}

	if err := healthCheckGreen(client, greenPort, versionDir); err != nil {
		return err
	}

	if err := switchToGreen(client, name, serviceDir, versionDir, greenPort); err != nil {
		return err
	}

	fmt.Printf("  → Blue/green complete. Now on port %d\n", greenPort)
	return nil
}

func startGreenContainer(client *goss.Client, versionDir string, greenPort int) error {
	greenComposePath := fmt.Sprintf("%s/docker-compose.green.yml", versionDir)

	greenCompose := fmt.Sprintf(`
services:
  app:
    extends:
      file: docker-compose.yml
      service: app
    ports:
      - "%d:%d"
`, greenPort, greenPort)

	if _, _, err := ssh.Run(client, fmt.Sprintf("cat > %s << 'EOF'\n%s\nEOF", greenComposePath, greenCompose)); err != nil { log.Printf("bluegreen: write compose error: %v", err) }

	fmt.Printf("  → Starting green container on port %d...\n", greenPort)
	_, _, err := ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose -f docker-compose.green.yml up -d 2>&1", versionDir))
	if err != nil {
		if _, _, rErr := ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose -f docker-compose.green.yml down 2>&1 || true", versionDir)); rErr != nil { log.Printf("bluegreen: rollback error: %v", rErr) }
		return fmt.Errorf("green start failed: %w", err)
	}
	return nil
}

func healthCheckGreen(client *goss.Client, greenPort int, versionDir string) error {
	fmt.Println("  → Health checking green container...")
	healthOK := false
	for range 15 {
		check, _, _ := ssh.Run(client, fmt.Sprintf("curl -sf http://localhost:%d/health 2>/dev/null || echo 'fail'", greenPort))
		if strings.TrimSpace(check) != "fail" {
			healthOK = true
			break
		}
		if _, _, err := ssh.Run(client, "sleep 2"); err != nil { log.Printf("bluegreen: sleep error: %v", err) }
	}

	if !healthOK {
		fmt.Println("  ✗ Health check failed, rolling back green...")
		if _, _, err := ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose -f docker-compose.green.yml down 2>&1 || true", versionDir)); err != nil { log.Printf("bluegreen: rollback error: %v", err) }
		return fmt.Errorf("green health check failed")
	}
	return nil
}

func switchToGreen(client *goss.Client, name, serviceDir, versionDir string, greenPort int) error {
	fmt.Println("  → Switching traffic to green...")
	proxyType := DetectProxy(client)
	if proxyType != "" {
		proxy := NewProxy(proxyType)
		domains := getAppDomains(client, name)
		for _, domain := range domains {
			if err := proxy.UpdateTargetPort(client, domain, greenPort); err != nil { log.Printf("bluegreen: update target port error: %v", err) }
		}
	}

	fmt.Println("  → Stopping old (blue) container...")
	greenComposePath := fmt.Sprintf("%s/docker-compose.green.yml", versionDir)
	if _, _, err := ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose down 2>&1 || true", versionDir)); err != nil { log.Printf("bluegreen: stop error: %v", err) }
	if _, _, err := ssh.Run(client, fmt.Sprintf("rm -f %s", greenComposePath)); err != nil { log.Printf("bluegreen: remove error: %v", err) }

	if _, _, err := ssh.Run(client, fmt.Sprintf("ln -sfn %s %s/current", versionDir, serviceDir)); err != nil { log.Printf("bluegreen: symlink error: %v", err) }

	return nil
}

func getCurrentPort(client *goss.Client, name string) int {
	out, _, _ := ssh.Run(client, fmt.Sprintf(
		`sudo docker compose -f /opt/sdk-ops/services/%s/current/docker-compose.yml port app 2>/dev/null || echo "0"`, name))
	out = strings.TrimSpace(out)
	if out != "" && out != "0" {
		parts := strings.Split(out, ":")
		if len(parts) > 1 {
			if p, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
				return p
			}
		}
	}

	caddyOut, _, _ := ssh.Run(client, `grep -oP 'reverse_proxy localhost:\K\d+' /etc/caddy/Caddyfile 2>/dev/null || echo "8080"`)
	caddyOut = strings.TrimSpace(caddyOut)
	if p, err := strconv.Atoi(caddyOut); err == nil {
		return p
	}
	return 8080
}

func getAppDomains(client *goss.Client, name string) []string {
	out, _, _ := ssh.Run(client, `grep -oP '^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}' /etc/caddy/Caddyfile 2>/dev/null | head -1 || echo ""`)
	domain := strings.TrimSpace(out)
	if domain != "" {
		return []string{domain}
	}
	return []string{name + ".local"}
}
