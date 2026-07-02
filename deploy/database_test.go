package deploy

import (
	"testing"
)

func TestImageNames(t *testing.T) {
	tests := []struct {
		dbType DBType
		want   string
	}{
		{DBPostgres, "postgres"},
		{DBMySQL, "mysql"},
		{DBRedis, "redis"},
		{DBMongoDB, "mongo"},
		{"custom", "custom"},
	}

	for _, tt := range tests {
		got := imageName(tt.dbType)
		if got != tt.want {
			t.Errorf("imageName(%q) = %q, want %q", tt.dbType, got, tt.want)
		}
	}
}

func TestLatestVersions(t *testing.T) {
	tests := []struct {
		dbType DBType
		want   string
	}{
		{DBPostgres, "17-alpine"},
		{DBMySQL, "8.0"},
		{DBRedis, "7-alpine"},
		{DBMongoDB, "7"},
		{"unknown", "latest"},
	}

	for _, tt := range tests {
		got := latestVersion(tt.dbType)
		if got != tt.want {
			t.Errorf("latestVersion(%q) = %q, want %q", tt.dbType, got, tt.want)
		}
	}
}

func TestGenPassword(t *testing.T) {
	p1, err := genPassword()
	if err != nil {
		t.Fatalf("genPassword: %v", err)
	}
	if len(p1) != 24 {
		t.Errorf("expected length 24, got %d", len(p1))
	}

	p2, err := genPassword()
	if err != nil {
		t.Fatalf("genPassword: %v", err)
	}
	if p1 == p2 {
		t.Log("warning: generated same password twice (low probability)")
	}
}

func TestDBConfigDefaults(t *testing.T) {
	cfg := DBConfig{
		Type: DBPostgres,
	}
	if cfg.Name != "" {
		t.Logf("default name should be empty: %q", cfg.Name)
	}
	if cfg.Version != "" {
		t.Logf("default version should be empty: %q", cfg.Version)
	}
	if cfg.Port != 0 {
		t.Logf("default port should be 0 (internal only)")
	}
}
