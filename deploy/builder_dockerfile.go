package deploy

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type DockerfileBuilder struct{}

func (b *DockerfileBuilder) Type() BuilderType {
	return BuilderDockerfile
}

func (b *DockerfileBuilder) Detect(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
		return true
	}
	return false
}

func (b *DockerfileBuilder) Build(dir, name string, reg RegistryConfig) (string, error) {
	existingDockerfile := filepath.Join(dir, "Dockerfile")
	hasDockerfile := false
	if _, err := os.Stat(existingDockerfile); err == nil {
		hasDockerfile = true
	}

	tag := fmt.Sprintf("%s/%s:v1.0.0", reg.Server, name)

	if !hasDockerfile {
		mainGo := filepath.Join(dir, "main.go")
		if _, err := os.Stat(mainGo); os.IsNotExist(err) {
			return "", fmt.Errorf("no Dockerfile or main.go found in %s", dir)
		}

		binaryName := fmt.Sprintf("sdk-ops-%s-amd64", name)
		binaryPath := filepath.Join(dir, binaryName)

		fmt.Printf("  → Building Go binary for linux/amd64...\n")
		build := exec.CommandContext(context.Background(), "go")
		build.Args = append(build.Args, "build", "-a", "-o", binaryPath, "-ldflags=-s -w", ".")
		build.Dir = dir
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
		if err := build.Run(); err != nil {
			return "", fmt.Errorf("go build: %w", err)
		}

		dockerfileContent := fmt.Sprintf(`FROM debian:stable-slim
RUN apt-get update -qq && apt-get install -y -qq ca-certificates && rm -rf /var/lib/apt/lists/*
ARG CACHEBUST=1
COPY %s /app
EXPOSE 8080
CMD ["/app"]
`, binaryName)

		dockerfilePath := filepath.Join(dir, "Dockerfile.deploy")
		if err := os.WriteFile(filepath.Clean(dockerfilePath), []byte(dockerfileContent), 0600); err != nil {
			return "", fmt.Errorf("write Dockerfile: %w", err)
		}
		defer func() { if err := os.Remove(dockerfilePath); err != nil { log.Printf("builder: remove error: %v", err) } }()
		defer func() { if err := os.Remove(binaryPath); err != nil { log.Printf("builder: remove error: %v", err) } }()

		return b.buildAndPush(dir, name, reg, tag, dockerfilePath)
	}

	return b.buildAndPush(dir, name, reg, tag, existingDockerfile)
}

func (b *DockerfileBuilder) buildAndPush(dir, name string, reg RegistryConfig, tag, dockerfilePath string) (string, error) {
	fmt.Printf("  → Logging in to %s...\n", reg.Server)
	login := exec.CommandContext(context.Background(), "docker")
	login.Args = append(login.Args, "login", reg.Server, "-u", reg.Username, "-p", reg.Password)
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr
	if err := login.Run(); err != nil {
		return "", fmt.Errorf("docker login: %w", err)
	}

	versionTag := fmt.Sprintf("%s/%s:latest", reg.Server, name)
	buildID := fmt.Sprintf("%d", time.Now().UnixNano())

	fmt.Printf("  → Building + pushing %s...\n", tag)
	push := exec.CommandContext(context.Background(), "docker")
	push.Args = append(push.Args, "buildx", "build",
		"--platform", "linux/amd64",
		"--build-arg", fmt.Sprintf("CACHEBUST=%s", buildID),
		"-f", dockerfilePath,
		"-t", tag, "-t", versionTag,
		"--push", dir)
	push.Stdout = os.Stdout
	push.Stderr = os.Stderr
	if err := push.Run(); err != nil {
		return "", fmt.Errorf("docker buildx: %w", err)
	}

	return tag, nil
}
