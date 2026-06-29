package monitor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{2048, "2.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBar(t *testing.T) {
	// 50% filled
	b := bar(50, 20)
	if utf8.RuneCountInString(b) != 20 {
		t.Errorf("bar(50,20) rune count = %d, want %d", utf8.RuneCountInString(b), 20)
	}
	if !strings.Contains(b, "█") || !strings.Contains(b, "░") {
		t.Errorf("bar(50,20) should contain both filled and empty chars")
	}

	// 100% filled
	b = bar(100, 10)
	if strings.Contains(b, "░") {
		t.Errorf("bar(100,10) should be fully filled")
	}

	// 0% filled
	b = bar(0, 10)
	if strings.Contains(b, "█") {
		t.Errorf("bar(0,10) should be fully empty")
	}

	// Clamp > 100
	b = bar(150, 10)
	if strings.Contains(b, "░") {
		t.Errorf("bar(150,10) should be clamped to fully filled")
	}

	// Clamp < 0
	b = bar(-10, 10)
	if strings.Contains(b, "█") {
		t.Errorf("bar(-10,10) should be clamped to fully empty")
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"active", "✅"},
		{"yes", "✅"},
		{"OK", "✅"},
		{"inactive", "❌"},
		{"failed", "❌"},
		{"", "❌"},
	}
	for _, tt := range tests {
		got := statusIcon(tt.status)
		if got != tt.want {
			t.Errorf("statusIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestNodeStatsDefaults(t *testing.T) {
	stats := &NodeStats{}
	if stats.Hostname != "" {
		t.Error("Hostname should default to empty string")
	}
	if stats.CPUCores != 0 {
		t.Error("CPUCores should default to 0")
	}
}

func TestRuntimeStatusDefaults(t *testing.T) {
	rs := &RuntimeStatus{}
	if rs.K3sVersion != "" {
		t.Error("K3sVersion should default to empty string")
	}
	if rs.DockerOK != "" {
		t.Error("DockerOK should default to empty string")
	}
}
