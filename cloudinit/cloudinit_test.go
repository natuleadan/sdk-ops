package cloudinit

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.User != "sdkops" {
		t.Errorf("default User = %q, want %q", cfg.User, "sdkops")
	}
	if cfg.SSHPort != 2222 {
		t.Errorf("default SSHPort = %d, want %d", cfg.SSHPort, 2222)
	}
	if cfg.Mode != "k3s" {
		t.Errorf("default Mode = %q, want %q", cfg.Mode, "k3s")
	}
}

func TestGenerate_Basic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "bare"
	cfg.SSHKeys = []string{"ssh-ed25519 key123"}
	result := Generate(cfg)

	if !strings.Contains(result, "#cloud-config") {
		t.Error("missing #cloud-config header")
	}
	if !strings.Contains(result, "sdkops") {
		t.Error("missing user name")
	}
	if !strings.Contains(result, "nftables") {
		t.Error("missing nftables")
	}
	if !strings.Contains(result, "fail2ban") {
		t.Error("missing fail2ban")
	}
	if !strings.Contains(result, "ssh_authorized_keys") {
		t.Error("missing ssh keys section")
	}
}

func TestGenerate_DockerMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "docker"
	result := Generate(cfg)

	if !strings.Contains(result, "get.docker.com") {
		t.Error("missing docker install in docker mode")
	}
	if strings.Contains(result, "get.k3s.io") {
		t.Error("should not include k3s in docker mode")
	}
}

func TestGenerate_K3sMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "k3s"
	result := Generate(cfg)

	if !strings.Contains(result, "get.docker.com") {
		t.Error("missing docker install in k3s mode")
	}
	if !strings.Contains(result, "get.k3s.io") {
		t.Error("missing k3s install in k3s mode")
	}
}

func TestGenerate_WithCrowdSec(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CrowdSec = true
	result := Generate(cfg)

	if !strings.Contains(result, "crowdsec") {
		t.Error("missing crowdsec install")
	}
}

func TestGenerate_WithDisableTraefik(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DisableTraefik = true
	result := Generate(cfg)

	if !strings.Contains(result, "--disable traefik") {
		t.Error("missing --disable traefik flag")
	}
}

func TestGenerate_WithMonitor(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableMonitor = true
	cfg.SSHKeys = []string{"ssh-ed25519 key123"}
	result := Generate(cfg)

	if !strings.Contains(result, "node_exporter") {
		t.Error("missing node_exporter install")
	}
	if !strings.Contains(result, ":9100") {
		t.Error("missing port 9100")
	}
}

func TestGenerate_SSHKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SSHKeys = []string{"ssh-ed25519 AAAAC3...", "ssh-rsa AAAAB3..."}
	result := Generate(cfg)

	if !strings.Contains(result, "ssh-ed25519 AAAAC3...") {
		t.Error("missing first SSH key")
	}
	if !strings.Contains(result, "ssh-rsa AAAAB3...") {
		t.Error("missing second SSH key")
	}
}

func TestValidateMode(t *testing.T) {
	if err := ValidateMode("k3s"); err != nil {
		t.Errorf("k3s should be valid: %v", err)
	}
	if err := ValidateMode("docker"); err != nil {
		t.Errorf("docker should be valid: %v", err)
	}
	if err := ValidateMode("bare"); err != nil {
		t.Errorf("bare should be valid: %v", err)
	}
	if err := ValidateMode("invalid"); err == nil {
		t.Error("invalid should be rejected")
	}
}

func TestGenerate_NoRootSSHKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SSHKeys = nil
	result := Generate(cfg)

	if strings.Contains(result, "ssh_authorized_keys: []") || strings.Contains(result, "ssh_authorized_keys:\n") {
		t.Log("empty SSH keys section present (may cause cloud-init warnings)")
	}
}

func TestGenerate_CustomUser(t *testing.T) {
	cfg := DefaultConfig()
	cfg.User = "admin"
	result := Generate(cfg)

	if !strings.Contains(result, "name: admin") {
		t.Errorf("expected user 'admin', got: %s", result)
	}
}

func TestGenerate_CustomPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SSHPort = 2223
	result := Generate(cfg)

	if !strings.Contains(result, "Port 2223") {
		t.Errorf("expected Port 2223, got: %s", result)
	}
}
