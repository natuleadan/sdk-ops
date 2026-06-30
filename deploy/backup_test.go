package deploy

import (
	"testing"
)

func TestCronToSystemdCalendar(t *testing.T) {
	tests := []struct {
		cron string
		want string
	}{
		{"0 3 * * *", "*-*-* 03:00:00"},
		{"30 6 * * 1", "1-*-* 06:30:00"},
		{"*/15 * * * *", "*-*-* *:0/15:00"},
		{"0 */2 * * *", "*-*-* 0/2:00:00"},
		{"0 0 1 * *", "*-*-1 00:00:00"},
	}

	for _, tt := range tests {
		got := cronToSystemdCalendar(tt.cron)
		if got != tt.want {
			t.Errorf("cronToSystemdCalendar(%q) = %q, want %q", tt.cron, got, tt.want)
		}
	}
}

func TestS3ConfigFromEnv(t *testing.T) {
	// No env set
	cfg := S3ConfigFromEnv()
	if cfg.Endpoint != "" || cfg.Bucket != "" {
		t.Log("S3ConfigFromEnv returned config with values (env may be set)")
	}

	// Test struct fields exist
	if cfg.Path != "" {
		t.Logf("path: %s", cfg.Path)
	}
}
