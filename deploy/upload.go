package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	goss "golang.org/x/crypto/ssh"

	"github.com/natuleadan/sdk-ops/ssh"
)

type UploadConfig struct {
	ServiceName string
	SourceDir   string   // Local directory to upload
	Files       []string // Specific files to include (empty = all)
	Exclude     []string // Patterns to exclude
}

type DeployResult struct {
	Version     string
	ServicePath string
}

func UploadAndDeploy(client *goss.Client, cfg UploadConfig) (*DeployResult, error) {
	serviceDir := fmt.Sprintf("/opt/sdk-ops/services/%s", cfg.ServiceName)

	// Ensure service directory on VPS
	ssh.Run(client, fmt.Sprintf("sudo mkdir -p %s", serviceDir))
	ssh.Run(client, fmt.Sprintf("sudo chown -R $(whoami) /opt/sdk-ops 2>/dev/null || true"))

	// Determine next version number
	verOut, _, err := ssh.Run(client, fmt.Sprintf(`
		ls -d %s/v* 2>/dev/null | sed 's/.*v//' | sort -n | tail -1
	`, serviceDir))
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	verOut = strings.TrimSpace(verOut)
	nextVer := "1"
	if verOut != "" {
		fmt.Sscanf(verOut, "%d", &nextVer)
		nextVer = fmt.Sprintf("%d", atoi(nextVer)+1)
	}

	versionDir := fmt.Sprintf("%s/v%s", serviceDir, nextVer)

	// Create tar.gz in memory
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cfg.SourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Check exclude
		for _, excl := range cfg.Exclude {
			if matched, _ := filepath.Match(excl, rel); matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		// Filter specific files
		if len(cfg.Files) > 0 {
			found := false
			for _, f := range cfg.Files {
				if rel == f || strings.HasPrefix(rel, f+"/") {
					found = true
					break
				}
			}
			if !found {
				if info.IsDir() {
					return nil
				}
				return nil
			}
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if info.IsDir() {
			header.Name += "/"
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.IsDir() {
			f, err := os.Open(filepath.Clean(path))
			if err != nil {
				return err
			}
			defer f.Close()
			io.Copy(tw, f)
		}
		return nil
	}

	if err := filepath.Walk(cfg.SourceDir, walkFn); err != nil {
		return nil, fmt.Errorf("walk source: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	// Upload tar.gz via SSH
	fmt.Printf("  → Uploading %d bytes...\n", buf.Len())
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	extractCmd := fmt.Sprintf("sudo mkdir -p %s && sudo tar xzf - -C %s && sudo chown -R $(whoami) %s", versionDir, versionDir, versionDir)
	session.Stdin = &buf
	out, err := session.CombinedOutput(extractCmd)
	if err != nil {
		return nil, fmt.Errorf("extract failed: %w\n%s", err, string(out))
	}

	// Update symlinks
	ssh.Run(client, fmt.Sprintf(
		`sudo ln -sfn %s %s/previous 2>/dev/null; sudo ln -sfn %s %s/current; echo "deployed"`,
		fmt.Sprintf("%s/current", serviceDir),
		serviceDir,
		versionDir,
		serviceDir,
	))

	fmt.Printf("  → Deployed v%s to %s\n", nextVer, versionDir)
	return &DeployResult{Version: nextVer, ServicePath: versionDir}, nil
}

func Rollback(client *goss.Client, serviceName, targetVersion string) error {
	serviceDir := fmt.Sprintf("/opt/sdk-ops/services/%s", serviceName)

	var script string
	if targetVersion != "" {
		// Rollback to a specific version
		targetDir := fmt.Sprintf("%s/%s", serviceDir, targetVersion)
		script = fmt.Sprintf(`
TARGET="%s"
CURRENT=$(sudo readlink -f %s/current 2>/dev/null || echo "")
if [ ! -d "$TARGET" ]; then
	echo "version-not-found"
	exit 0
fi
sudo ln -sfn "$TARGET" %s/current
sudo ln -sfn "$CURRENT" %s/previous
echo "rolled-back to $(basename $TARGET)"
`, targetDir, serviceDir, serviceDir, serviceDir)
	} else {
		// Rollback to previous (default behavior)
		script = fmt.Sprintf(`
CURRENT=$(sudo readlink -f %s/current 2>/dev/null || echo "")
PREVIOUS=$(sudo readlink -f %s/previous 2>/dev/null || echo "")
if [ -z "$PREVIOUS" ] || [ ! -d "$PREVIOUS" ]; then
	echo "no-previous"
	exit 0
fi
sudo ln -sfn "$PREVIOUS" %s/current
sudo ln -sfn "$CURRENT" %s/previous
echo "rolled-back to $(basename $PREVIOUS)"
`, serviceDir, serviceDir, serviceDir, serviceDir)
	}

	out, _, err := ssh.Run(client, script)
	if err != nil {
		return fmt.Errorf("rollback exec: %w\n%s", err, out)
	}
	out = strings.TrimSpace(out)
	if out == "no-previous" {
		return fmt.Errorf("no previous version to rollback to for %s", serviceName)
	}
	if out == "version-not-found" {
		return fmt.Errorf("version %s not found for %s", targetVersion, serviceName)
	}
	fmt.Printf("  → %s: %s\n", serviceName, out)
	return nil
}

func DiffVersions(client *goss.Client, serviceName, verA, verB string) (string, error) {
	dirA := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", serviceName, verA)
	dirB := fmt.Sprintf("/opt/sdk-ops/services/%s/%s", serviceName, verB)

	out, _, err := ssh.Run(client, fmt.Sprintf(
		`diff -rq "%s" "%s" 2>/dev/null || true`, dirA, dirB))
	if err != nil {
		return "", fmt.Errorf("diff versions: %w", err)
	}
	return out, nil
}

func ListVersions(client *goss.Client, serviceName string) ([]string, error) {
	out, _, err := ssh.Run(client, fmt.Sprintf(
		`ls -d /opt/sdk-ops/services/%s/v* 2>/dev/null | sed 's/.*v/v/' | sort -t'v' -k2 -n`, serviceName))
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

type RegistryConfig struct {
	Server   string
	Username string
	Password string
}

func (r RegistryConfig) Valid() bool {
	return r.Server != "" && !strings.ContainsAny(r.Server, " \t\n\r;|&") &&
		r.Username != "" && !strings.ContainsAny(r.Username, " \t\n\r;|&") &&
		r.Password != ""
}

func DefaultRegistry() RegistryConfig {
	return RegistryConfig{
		Server:   os.Getenv("REGISTRY_SERVER"),
		Username: os.Getenv("REGISTRY_USER"),
		Password: os.Getenv("REGISTRY_PASS"),
	}
}

// GenerateCompose generates a docker-compose.yml with optional postgres sidecar
func GenerateCompose(imageRef, serviceName string, port int, hasDB bool) []byte {
	var buf bytes.Buffer
	if port == 0 {
		port = 8080
	}

	fmt.Fprintf(&buf, `services:
  %[1]s:
    image: %[2]s
    ports:
      - "%[3]d:%[3]d"
    working_dir: /app
`, serviceName, imageRef, port)

	if hasDB {
		fmt.Fprintf(&buf, `    volumes:
      - ./service.yaml:/app/service.yaml
    depends_on:
      %[1]s-db:
        condition: service_healthy
    restart: unless-stopped

  %[1]s-db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: %[1]s
      POSTGRES_PASSWORD: %[1]s
      POSTGRES_DB: %[1]s
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U %[1]s"]
      interval: 5s
      timeout: 5s
      retries: 5
    restart: unless-stopped
`, serviceName)
	} else {
		buf.WriteString(`    restart: unless-stopped
`)
	}

	return buf.Bytes()
}

// BuildAndPushImage builds a Go binary locally, wraps in minimal Docker image, pushes to registry
func BuildAndPushImage(dir, name string, reg RegistryConfig) (string, error) {
	mainGo := filepath.Join(dir, "main.go")
	if _, err := os.Stat(mainGo); os.IsNotExist(err) {
		return "", fmt.Errorf("no main.go in %s", dir)
	}

	binaryName := fmt.Sprintf("sdk-ops-%s-amd64", name)
	binaryPath := filepath.Join(dir, binaryName)

	tag := fmt.Sprintf("%s/%s:v1.0.0", reg.Server, name)
	versionTag := fmt.Sprintf("%s/%s:latest", reg.Server, name)

	// Step 1: Build Go binary for linux/amd64
	fmt.Printf("  → Building Go binary for linux/amd64...\n")
	build := exec.Command("go", "build",
		"-a", // force rebuild of all dependencies
		"-o", binaryPath,
		"-ldflags=-s -w",
		".")
	build.Dir = dir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("go build: %w", err)
	}

	// Step 2: Create minimal Dockerfile that just copies the binary
	dockerfileContent := fmt.Sprintf(`FROM debian:stable-slim
RUN apt-get update -qq && apt-get install -y -qq ca-certificates && rm -rf /var/lib/apt/lists/*
ARG CACHEBUST=1
COPY %s /healthz-svc
EXPOSE 8080
CMD ["/healthz-svc"]
`, binaryName)

	dockerfilePath := filepath.Join(dir, "Dockerfile.deploy")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0600); err != nil {
		return "", fmt.Errorf("write Dockerfile: %w", err)
	}

	// Step 3: Login to registry
	fmt.Printf("  → Logging in to %s...\n", reg.Server)
	login := exec.CommandContext(context.Background(), "docker", "login", reg.Server,
		"-u", reg.Username,
		"-p", reg.Password)
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr
	if err := login.Run(); err != nil {
		return "", fmt.Errorf("docker login: %w", err)
	}

	// Step 4: Build and push for linux/amd64
	fmt.Printf("  → Building + pushing %s...\n", tag)
	buildID := fmt.Sprintf("%d", time.Now().UnixNano())
	push := exec.Command("docker", "buildx", "build",
		"--platform", "linux/amd64",
		"--build-arg", fmt.Sprintf("CACHEBUST=%s", buildID),
		"-f", dockerfilePath,
		"-t", tag,
		"-t", versionTag,
		"--push",
		dir)
	push.Stdout = os.Stdout
	push.Stderr = os.Stderr
	if err := push.Run(); err != nil {
		return "", fmt.Errorf("docker buildx: %w", err)
	}

	// Cleanup
	os.Remove(dockerfilePath)
	os.Remove(binaryPath)

	fmt.Printf("  → Image pushed: %s\n", tag)
	return tag, nil
}

// UploadImage uploads a Docker image tar.gz to the VPS and loads it
func UploadImage(client *goss.Client, serviceName, version string, imageTar []byte) (string, error) {
	remotePath := fmt.Sprintf("/opt/sdk-ops/services/%s/v%s/image.tar.gz", serviceName, version)
	
	// Upload via SSH pipe
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	defer session.Close()

	fmt.Printf("  → Uploading Docker image (%d bytes)...\n", len(imageTar))
	loadCmd := fmt.Sprintf("sudo tee %s > /dev/null && sudo docker load < %s && echo 'image_loaded'", remotePath, remotePath)
	session.Stdin = bytes.NewReader(imageTar)
	out, err := session.CombinedOutput(loadCmd)
	if err != nil {
		return "", fmt.Errorf("upload/load image: %w\n%s", err, string(out))
	}
	fmt.Print(string(out))
	return remotePath, nil
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
