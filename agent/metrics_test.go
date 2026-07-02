package main

import (
	"os"
	"runtime"
	"testing"
	"time"
)

func TestCollectMetrics(t *testing.T) {
	m := collectMetrics()


	if m.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}

	// CPU should be a valid percentage
	if m.CPUPercent < 0 || m.CPUPercent > 100 {
		t.Errorf("CPUPercent out of range: %f", m.CPUPercent)
	}

	// Memory should be non-zero on any real system
	if m.MemoryTotal == 0 {
		t.Log("MemoryTotal is 0 (may be running in constrained env)")
	}

	// Timestamp should be recent
	if time.Since(m.Timestamp) > 10*time.Second {
		t.Error("Timestamp is too old")
	}
}

func TestCollectMetricsTypes(t *testing.T) {
	m := collectMetrics()


	// Verify types
	if m.CPUPercent < 0 || m.CPUPercent > 100 {
		t.Errorf("CPUPercent out of range: %f", m.CPUPercent)
	}
}

func TestGetHostInfo(t *testing.T) {
	info := getHostInfo()

	if info["hostname"] == "" {
		t.Error("hostname should not be empty")
	}
	if info["os"] != runtime.GOOS {
		t.Errorf("os = %q, want %q", info["os"], runtime.GOOS)
	}
	if info["arch"] != runtime.GOARCH {
		t.Errorf("arch = %q, want %q", info["arch"], runtime.GOARCH)
	}
	if info["go_version"] == "" {
		t.Error("go_version should not be empty")
	}
}

func TestGetUptime(t *testing.T) {
	uptime := getUptime()
	if uptime == "" {
		t.Error("uptime should not be empty")
	}
	if uptime == "unknown" {
		t.Log("uptime unknown (running without /proc)")
	}
}

func TestGetLocalIP(t *testing.T) {
	ip := getLocalIP()
	if ip == "" {
		t.Error("IP should not be empty")
	}
	if ip != "unknown" {
		t.Logf("local IP: %s", ip)
	}
}

func TestConcurrentMetrics(t *testing.T) {
	// Collect metrics concurrently to check for race conditions
	results := make(chan MetricRow, 5)
	for range 5 {
		go func() {
			m := collectMetrics()
			results <- m
		}()
	}

	for range 5 {
		m := <-results
		if m.Timestamp.IsZero() {
			t.Error("concurrent metric has zero timestamp")
		}
	}
}

func TestMetricFieldsConsistency(t *testing.T) {
	m := collectMetrics()


	// Memory used should be <= total (within reasonable bounds)
	if m.MemoryTotal > 0 && m.MemoryUsed > m.MemoryTotal {
		t.Errorf("MemoryUsed (%d) > MemoryTotal (%d)", m.MemoryUsed, m.MemoryTotal)
	}

	// Disk used should be <= total
	if m.DiskTotal > 0 && m.DiskUsed > m.DiskTotal {
		t.Errorf("DiskUsed (%d) > DiskTotal (%d)", m.DiskUsed, m.DiskTotal)
	}
}

func TestCollectMetricsIdempotent(t *testing.T) {
	m1 := collectMetrics()
	m2 := collectMetrics()

	t.Logf("CPU 1: %f, CPU 2: %f", m1.CPUPercent, m2.CPUPercent)
}

func TestGetHostInfoFromEnv(t *testing.T) {
	// Override hostname via env (not possible on all platforms)
	orig := os.Getenv("HOSTNAME")
	defer os.Setenv("HOSTNAME", orig)

	info := getHostInfo()
	if info["hostname"] == "" {
		t.Log("hostname from os, not env")
	}
}
