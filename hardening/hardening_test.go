package hardening

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.User != "sdkops" {
		t.Errorf("default User = %q, want %q", cfg.User, "sdkops")
	}
	if cfg.SSHPort != 0 {
		t.Errorf("default SSHPort = %d, want 0 (no migration)", cfg.SSHPort)
	}
}

func TestConfigMigrate(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MigrateSSH() {
		t.Error("default should not migrate SSH")
	}
	cfg.SSHPort = 2222
	if !cfg.MigrateSSH() {
		t.Error("SSHPort=2222 should trigger migration")
	}
	cfg.SSHPort = 22
	if cfg.MigrateSSH() {
		t.Error("SSHPort=22 should not trigger migration")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		sshPort  int
		wantUser string
	}{
		{"default", "", 0, "sdkops"},
		{"custom", "admin", 2222, "admin"},
		{"root", "root", 2222, "root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			if tt.user != "" {
				cfg.User = tt.user
			}
			if cfg.User != tt.wantUser {
				t.Errorf("User = %q, want %q", cfg.User, tt.wantUser)
			}
		})
	}
}
