package deploy

import (
	"fmt"
	"strings"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

func DeploySwarm(client *goss.Client, name, versionDir, imageRef string) error {
	if imageRef == "" {
		return fmt.Errorf("image required for swarm deploy")
	}

	// Init swarm if not already
	out, _, _ := ssh.Run(client, "docker info --format '{{.Swarm.LocalNodeState}}' 2>/dev/null || echo inactive")
	if strings.TrimSpace(out) != "active" {
		if _, _, err := ssh.Run(client, "docker swarm init --advertise-addr eth0 2>/dev/null || true"); err != nil {
			return fmt.Errorf("swarm init: %w", err)
		}
	}

	stackName := name
	composePath := fmt.Sprintf("%s/docker-compose.yml", versionDir)

	// Check compose exists
	hasCompose, _, _ := ssh.Run(client, fmt.Sprintf("test -f %s && echo yes || echo no", composePath))
	if strings.TrimSpace(hasCompose) != "yes" {
		return fmt.Errorf("docker-compose.yml not found in %s", versionDir)
	}

	out, _, err := ssh.Run(client, fmt.Sprintf("docker stack deploy -c %s %s --with-registry-auth 2>&1", composePath, stackName))
	if err != nil {
		return fmt.Errorf("stack deploy: %w\n%s", err, out)
	}
	fmt.Printf("  → Swarm stack %q deployed\n", stackName)
	return nil
}

func RemoveSwarmStack(client *goss.Client, name string) error {
	out, _, err := ssh.Run(client, fmt.Sprintf("docker stack rm %s 2>&1 || true", name))
	if err != nil {
		return fmt.Errorf("stack rm: %w", err)
	}
	fmt.Printf("  → Swarm stack %q removed\n%s", name, out)
	return nil
}
