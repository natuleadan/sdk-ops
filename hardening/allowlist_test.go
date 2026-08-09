package hardening

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateCIDR(t *testing.T) {
	tests := []struct {
		in string
		v6 bool
		ok bool
	}{
		{"192.0.2.0/24", false, true},
		{"192.0.2.1", false, true},
		{"192.0.2.2", false, true},
		{"2001:db8::/32", true, true},
		{"2001:db8:1::/32", true, true},
		{"2001:db8::8888", true, true},
		{"", false, false},
		{"192.0.2.1/33", false, false},
		{"192.0.2.2/33", false, false},
		{"not-an-ip", false, false},
		{"2001:db8::/999", true, false},
	}
	for _, tt := range tests {
		v6, err := ValidateCIDR(tt.in)
		if tt.ok {
			if err != nil {
				t.Errorf("ValidateCIDR(%q) unexpected error: %v", tt.in, err)
			}
			if v6 != tt.v6 {
				t.Errorf("ValidateCIDR(%q) v6 = %v, want %v", tt.in, v6, tt.v6)
			}
		} else if err == nil {
			t.Errorf("ValidateCIDR(%q) expected error, got nil", tt.in)
		}
	}
}

func TestParseCIDRList(t *testing.T) {
	raw := `
198.51.100.0/24
203.0.113.0/24

2001:db8::/32
2001:db8:1::/32
`
	v4, v6, err := ParseCIDRList(raw)
	if err != nil {
		t.Fatalf("ParseCIDRList: %v", err)
	}
	if len(v4) != 2 || len(v6) != 2 {
		t.Fatalf("v4=%d v6=%d, want 2/2", len(v4), len(v6))
	}
	if v4[0] != "198.51.100.0/24" {
		t.Errorf("v4 not sorted: %v", v4)
	}
}

func TestParseCIDRListInvalid(t *testing.T) {
	if _, _, err := ParseCIDRList("192.0.2.0/24\n192.0.2.1/33"); err == nil {
		t.Error("expected error for invalid CIDR")
	}
}

func TestParseSource(t *testing.T) {
	src, err := ParseSource("cf")
	if err != nil || src.Kind != "cf" {
		t.Errorf("cf source: %+v err=%v", src, err)
	}
	src, err = ParseSource("url:https://example.com/ips.txt")
	if err != nil || src.Kind != "url" || src.Value != "https://example.com/ips.txt" {
		t.Errorf("url source: %+v err=%v", src, err)
	}
	src, err = ParseSource("dns:_cloud-netblocks.googleusercontent.com")
	if err != nil || src.Kind != "dns" {
		t.Errorf("dns source: %+v err=%v", src, err)
	}
	if _, err := ParseSource("ftp:x"); err == nil {
		t.Error("ftp source should fail")
	}
	if _, err := ParseSource(""); err == nil {
		t.Error("empty source should fail")
	}
	if src.String() != "dns:_cloud-netblocks.googleusercontent.com" {
		t.Errorf("String() = %q", src.String())
	}
}

func TestGenerateAllowlistConfigNormal(t *testing.T) {
	cfg := AllowlistConfig{
		Profile:  AllowlistNormal,
		SSHPorts: []int{22, 2222},
		Admin4:   []string{"203.0.113.10"},
		Source:   DefaultSource(),
	}
	out := GenerateAllowlistConfig(cfg)
	for _, want := range []string{
		"table inet filter {",
		"set allow4 { type ipv4_addr; flags interval; }",
		"set allow6 { type ipv6_addr; flags interval; }",
		"set admin4 { type ipv4_addr; flags interval; elements = { 203.0.113.10/32 } }",
		"policy drop;",
		"tcp dport { 22, 2222 } accept",
		"ip saddr @admin4 accept",
		"ip saddr @allow4 accept",
		"ip6 saddr @allow6 accept",
		"chain forward {",
		"ip saddr 10.0.0.0/8 accept",     // go-check:ignore-ip (RFC1918 subnets)
		"ip saddr 172.16.0.0/12 accept",  // go-check:ignore-ip (RFC1918 subnets)
		"ip saddr 192.168.0.0/16 accept", // go-check:ignore-ip (RFC1918 subnets)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("normal config missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "flush ruleset") {
		t.Error("config must not flush the whole ruleset (Docker tables)")
	}
	if strings.Contains(out, "set allow4 { type ipv4_addr; }") {
		t.Error("sets must use flags interval to hold prefix elements")
	}
	if strings.Contains(out, "admin6 { type ipv4") {
		t.Error("admin6 must be ipv6_addr")
	}
}

func TestGenerateAllowlistConfigStrict(t *testing.T) {
	cfg := AllowlistConfig{
		Profile:  AllowlistStrict,
		SSHPorts: []int{22},
		Admin4:   []string{"203.0.113.10"},
		Admin6:   []string{"2001:db8::1"},
		Source:   DefaultSource(),
	}
	out := GenerateAllowlistConfig(cfg)
	for _, want := range []string{
		"set admin4 { type ipv4_addr; flags interval; elements = { 203.0.113.10/32 } }",
		"set admin6 { type ipv6_addr; flags interval; elements = { 2001:db8::1/128 } }",
		"ip saddr @admin4 accept",
		"ip6 saddr @admin6 accept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("strict config missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "tcp dport") {
		t.Error("strict config must not open any port directly")
	}
}

func TestGenerateAllowlistConfigEmptyAdmin(t *testing.T) {
	cfg := AllowlistConfig{Profile: AllowlistStrict, SSHPorts: []int{22}, Source: DefaultSource()}
	out := GenerateAllowlistConfig(cfg)
	if !strings.Contains(out, "set admin4 { type ipv4_addr; flags interval; }") {
		t.Errorf("admin4 without elements rendered wrong\n%s", out)
	}
}

func TestGenerateAllowlistConfigOpenWeb(t *testing.T) {
	cfg := AllowlistConfig{Profile: AllowlistNormal, SSHPorts: []int{22}, Source: DefaultSource(), OpenWeb: true}
	out := GenerateAllowlistConfig(cfg)
	for _, want := range []string{
		"tcp dport { 80, 443 } accept",
		"tcp dport { 22 } accept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("open-web config missing %q\n%s", want, out)
		}
	}
	// cf mode: web ports must NOT be opened directly (gated by the allow sets)
	cfg.OpenWeb = false
	out = GenerateAllowlistConfig(cfg)
	if strings.Contains(out, "tcp dport { 80, 443 } accept") {
		t.Error("cf mode must not open 80/443 directly")
	}
}

func TestAllowlistUnexposeScriptPortBoundary(t *testing.T) {
	// The unexpose script must match "dport 80" but never "dport 8088".
	script := fmt.Sprintf(`HANDLES=$(sudo nft --handle list chain inet filter exposed 2>/dev/null | grep -E "dport %d([^0-9]|$)" | grep -oE 'handle [0-9]+' | awk '{print $2}')`, 80)
	for _, want := range []string{`dport 80([^0-9]|$)`} {
		if !strings.Contains(script, want) {
			t.Errorf("unexpose script missing port boundary %q\n%s", want, script)
		}
	}
	if strings.Contains(script, `dport 80"`) {
		t.Error("unexpose script must not use a bare dport match (matches 8088 too)")
	}
}

func TestNormalizeCIDR(t *testing.T) {
	tests := []struct{ in, want string }{
		{"203.0.113.10", "203.0.113.10/32"},
		{"192.0.2.0/24", "192.0.2.0/24"},
		{"2001:db8::1", "2001:db8::1/128"},
		{"2001:db8:2::/32", "2001:db8:2::/32"},
		{" 203.0.113.5 ", "203.0.113.5/32"},
	}
	for _, tt := range tests {
		if got := NormalizeCIDR(tt.in); got != tt.want {
			t.Errorf("NormalizeCIDR(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Policy note: IPs are ALWAYS explicit (CLI/YAML), never auto-detected, to
// avoid ban lockouts from rotating egress IPs. No DetectPublicIP exists.
