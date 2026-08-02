package main

import (
	"testing"

	"github.com/natuleadan/sdk-ops/hardening"
)

func TestParseAllowlistFlag(t *testing.T) {
	tests := []struct {
		in      string
		profile hardening.AllowlistProfile
		source  string
		wantErr bool
	}{
		{"cf", hardening.AllowlistNormal, "cf", false},
		{"url:https://example.com/ips.txt", hardening.AllowlistNormal, "url:https://example.com/ips.txt", false},
		{"dns:_cloud-netblocks.googleusercontent.com", hardening.AllowlistNormal, "dns:_cloud-netblocks.googleusercontent.com", false},
		{"strict", hardening.AllowlistStrict, "cf", false},
		{"strict:url:https://example.com/ips.txt", hardening.AllowlistStrict, "url:https://example.com/ips.txt", false},
		{"ftp:x", hardening.AllowlistNormal, "", true},
		{"strict:ftp:x", hardening.AllowlistNormal, "", true},
	}
	for _, tt := range tests {
		profile, source, err := parseAllowlistFlag(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseAllowlistFlag(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAllowlistFlag(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if profile != tt.profile || source != tt.source {
			t.Errorf("parseAllowlistFlag(%q) = (%v, %q), want (%v, %q)", tt.in, profile, source, tt.profile, tt.source)
		}
	}
}
