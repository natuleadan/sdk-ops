package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type PackBuilder struct{}

func (b *PackBuilder) Type() BuilderType {
	return BuilderPack
}

func (b *PackBuilder) Detect(dir string) bool {
	indicators := []string{
		"project.toml", "buildpack.yml",
		"package.json", "Gemfile", "requirements.txt",
		"go.mod", "main.go",
		"composer.json",
		"Procfile",
	}
	for _, f := range indicators {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

func (b *PackBuilder) Build(dir, name string, reg RegistryConfig) (string, error) {
	tag := fmt.Sprintf("%s/%s:v1.0.0", reg.Server, name)

	// Check if pack CLI is available
	if _, err := exec.LookPath("pack"); err != nil {
		return "", fmt.Errorf("pack CLI not found. Install with: brew install buildpacks/tap/pack")
	}

	// Login to registry
	fmt.Printf("  → Logging in to %s...\n", reg.Server)
	login := exec.CommandContext(context.Background(), "docker", "login", reg.Server, "-u", reg.Username, "-p", reg.Password)
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr
	if err := login.Run(); err != nil {
		return "", fmt.Errorf("docker login: %w", err)
	}

	fmt.Printf("  → Building with pack (CNB)...\n")
	args := []string{"build", tag,
		"--builder", "heroku/builder:24",
		"--platform", "linux/amd64",
		"--publish",
		"--path", dir,
	}

	cmd := exec.CommandContext(context.Background(), "pack", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pack build: %w", err)
	}

	return tag, nil
}
