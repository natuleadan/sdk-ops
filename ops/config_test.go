package ops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`host: 192.168.1.100
user: sdkops
ssh_key: /home/user/.ssh/id_ed25519
ssh_port: 2222
mode: k3s`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Host != "192.168.1.100" {
		t.Errorf("Host = %q, want %q", cfg.Host, "192.168.1.100")
	}
	if cfg.User != "sdkops" {
		t.Errorf("User = %q, want %q", cfg.User, "sdkops")
	}
	if cfg.SSHPort != 2222 {
		t.Errorf("SSHPort = %d, want %d", cfg.SSHPort, 2222)
	}
	if cfg.Mode != ModeK3s {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModeK3s)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`host: 10.0.0.1`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.User != "root" {
		t.Errorf("default User = %q, want %q", cfg.User, "root")
	}
	if cfg.SSHPort != 22 {
		t.Errorf("default SSHPort = %d, want %d", cfg.SSHPort, 22)
	}
	if cfg.Mode != ModeK3s {
		t.Errorf("default Mode = %q, want %q", cfg.Mode, ModeK3s)
	}
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_OPS_HOST", "envhost.local")
	defer os.Unsetenv("TEST_OPS_HOST")

	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`host: ${TEST_OPS_HOST}
user: root`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Host != "envhost.local" {
		t.Errorf("Host = %q, want %q", cfg.Host, "envhost.local")
	}
}

func TestLoadConfig_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`host: 10.0.0.1
mode: invalid`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
}

func TestLoadConfig_MissingHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	content := []byte(`user: root`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing host, got nil")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestModes(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
		want string
	}{
		{"k3s", ModeK3s, "k3s"},
		{"docker", ModeDocker, "docker"},
		{"bare", ModeBare, "bare"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mode) != tt.want {
				t.Errorf("Mode = %q, want %q", tt.mode, tt.want)
			}
		})
	}
}
