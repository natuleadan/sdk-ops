package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type NixpacksBuilder struct{}

func (b *NixpacksBuilder) Type() BuilderType {
	return BuilderNixpacks
}

func (b *NixpacksBuilder) Detect(dir string) bool {
	indicators := []string{
		"package.json", "index.js", "server.js", "app.js",
		"requirements.txt", "setup.py", "main.py", "app.py",
		"Gemfile", "main.rb",
		"Cargo.toml",
		"go.mod", "main.go",
		"composer.json",
		"index.html",
		"Procfile",
	}
	for _, f := range indicators {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

func (b *NixpacksBuilder) Build(dir, name string, reg RegistryConfig) (string, error) {
	tag := fmt.Sprintf("%s/%s:v1.0.0", reg.Server, name)
	versionTag := fmt.Sprintf("%s/%s:latest", reg.Server, name)

	// Check if nixpacks is available
	if _, err := exec.LookPath("nixpacks"); err != nil {
		fmt.Println("  → Installing nixpacks...")
		install := exec.Command("npx", "nixpacks", "--version")
		install.Stderr = os.Stderr
		if err := install.Run(); err != nil {
			install2 := exec.Command("npm", "install", "-g", "nixpacks")
			install2.Stdout = os.Stdout
			install2.Stderr = os.Stderr
			if err := install2.Run(); err != nil {
				return "", fmt.Errorf("install nixpacks failed: %w", err)
			}
		}
	}

	// Login to registry
	fmt.Printf("  → Logging in to %s...\n", reg.Server)
	login := exec.Command("docker", "login", reg.Server, "-u", reg.Username, "-p", reg.Password)
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr
	if err := login.Run(); err != nil {
		return "", fmt.Errorf("docker login: %w", err)
	}

	fmt.Printf("  → Building with nixpacks...\n")
	args := []string{"build", dir,
		"--name", tag,
		"--tags", tag, versionTag,
		"--platform", "linux/amd64",
		"--push",
	}

	cmd := exec.Command("nixpacks", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("nixpacks build: %w", err)
	}

	return tag, nil
}
