package deploy

import (
	"fmt"
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
	greenComposePath := fmt.Sprintf("%s/docker-compose.green.yml", versionDir)

	// Create green compose with different port
	greenCompose := fmt.Sprintf(`
services:
  app:
    extends:
      file: docker-compose.yml
      service: app
    ports:
      - "%d:%d"
`, greenPort, greenPort)

	ssh.Run(client, fmt.Sprintf("cat > %s << 'EOF'\n%s\nEOF", greenComposePath, greenCompose))

	// Start green container
	fmt.Printf("  → Starting green container on port %d...\n", greenPort)
	_, _, err := ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose -f docker-compose.green.yml up -d 2>&1", versionDir))
	if err != nil {
		ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose -f docker-compose.green.yml down 2>&1 || true", versionDir))
		return fmt.Errorf("green start failed: %w", err)
	}

	// Health check green
	fmt.Println("  → Health checking green container...")
	healthOK := false
	for range 15 {
		check, _, _ := ssh.Run(client, fmt.Sprintf("curl -sf http://localhost:%d/health 2>/dev/null || echo 'fail'", greenPort))
		if strings.TrimSpace(check) != "fail" {
			healthOK = true
			break
		}
		ssh.Run(client, "sleep 2")
	}

	if !healthOK {
		fmt.Println("  ✗ Health check failed, rolling back green...")
		ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose -f docker-compose.green.yml down 2>&1 || true", versionDir))
		return fmt.Errorf("green health check failed")
	}

	// Switch proxy to green port
	fmt.Println("  → Switching traffic to green...")
	proxyType := DetectProxy(client)
	if proxyType != "" {
		proxy := NewProxy(proxyType)
		domains := getAppDomains(client, name)
		for _, domain := range domains {
			proxy.UpdateTargetPort(client, domain, greenPort)
		}
	}

	// Stop old container (blue)
	fmt.Println("  → Stopping old (blue) container...")
	ssh.Run(client, fmt.Sprintf("cd %s && sudo docker compose down 2>&1 || true", versionDir))
	ssh.Run(client, fmt.Sprintf("rm -f %s", greenComposePath))

	// Update symlink
	ssh.Run(client, fmt.Sprintf("ln -sfn %s %s/current", versionDir, serviceDir))

	fmt.Printf("  → Blue/green complete. Now on port %d\n", greenPort)
	return nil
}

func getCurrentPort(client *goss.Client, name string) int {
	// Check current docker compose port
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

	// Fallback: check Caddy reverse proxy target
	caddyOut, _, _ := ssh.Run(client, `grep -oP 'reverse_proxy localhost:\K\d+' /etc/caddy/Caddyfile 2>/dev/null || echo "8080"`)
	caddyOut = strings.TrimSpace(caddyOut)
	if p, err := strconv.Atoi(caddyOut); err == nil {
		return p
	}
	return 8080
}

func getAppDomains(client *goss.Client, name string) []string {
	// Get domains from Caddyfile
	out, _, _ := ssh.Run(client, `grep -oP '^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}' /etc/caddy/Caddyfile 2>/dev/null | head -1 || echo ""`)
	domain := strings.TrimSpace(out)
	if domain != "" {
		return []string{domain}
	}
	return []string{name + ".local"}
}
