package main

import (
	"strings"
	"testing"
)

func TestValidateProvision(t *testing.T) {
	ok := &ProvisionFile{
		Mode: "docker",
		Groups: map[string]GroupConfig{
			"edge": {
				Security: &SecurityConfig{Enabled: true, Threshold: 5},
				Traefik:  &TraefikConfig{Enabled: true, Domains: []TraefikDomain{{Domain: "app.example.com", Port: 8088}}},
				Tags:     []string{"dmz"},
			},
		},
		Hosts: []ProvisionHost{
			{Name: "a", Host: "192.0.2.1", Group: "edge"},
			{Name: "b", Host: "2001:db8::1"},
		},
		Peers: []ProvisionPeer{
			{From: "a", To: "b", Ports: []int{43453}},
		},
		SSL: SSLConfig{Email: "admin@x.com"},
	}
	names, err := validateProvision(ok)
	if err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	if names["b"] != "2001:db8::1" {
		t.Errorf("names map wrong: %v", names)
	}

	bad := []*ProvisionFile{
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1", Group: "nope"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, Peers: []ProvisionPeer{{From: "x", To: "a", Ports: []int{1}}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, Peers: []ProvisionPeer{{From: "a", To: "a"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, DeployOrder: []DeployStep{{Host: "ghost"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, DeployOrder: []DeployStep{{}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, Groups: map[string]GroupConfig{"g": {Traefik: &TraefikConfig{Domains: []TraefikDomain{{Domain: "x.com"}}}}}},
		{Mode: "docker", HTTPSMode: "open", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1", HTTPSMode: "weird"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, Traefik: TraefikConfig{Domains: []TraefikDomain{{Domain: "*.x.com", Service: "s", Port: 80, Wildcard: true}}}, SSL: SSLConfig{Email: "a@b.c"}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}, Traefik: TraefikConfig{Domains: []TraefikDomain{{Domain: "*.x.com", Service: "s", Port: 80, Wildcard: true}}}, SSL: SSLConfig{Email: "a@b.c", DNS01: &DNS01Config{Provider: "cloudflare"}}},
	}
	for i, pf := range bad {
		if _, err := validateProvision(pf); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestValidateHTTPSModeAndWildcard(t *testing.T) {
	// valid: https_mode cf/all + wildcard with dns01 token
	ok := &ProvisionFile{
		Mode:      "docker",
		HTTPSMode: "all",
		Hosts:     []ProvisionHost{{Name: "a", Host: "192.0.2.1"}},
		Traefik:   TraefikConfig{Domains: []TraefikDomain{{Domain: "*.x.com", Service: "s", Port: 80, Wildcard: true}}},
		SSL:       SSLConfig{Email: "a@b.c", DNS01: &DNS01Config{Provider: "cloudflare", APIToken: "tok"}},
	}
	if _, err := validateProvision(ok); err != nil {
		t.Fatalf("valid https_mode/wildcard plan rejected: %v", err)
	}
	// host override of https_mode is honoured
	pf := &ProvisionFile{Mode: "docker", HTTPSMode: "cf", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1", HTTPSMode: "all"}}}
	r := resolveHostConfig(pf, pf.Hosts[0])
	if r.httpsMode != "all" {
		t.Errorf("host https_mode override lost: %q", r.httpsMode)
	}
}

func TestResolveHostConfigPrecedence(t *testing.T) {
	pf := &ProvisionFile{
		FirewallAllowlist: "cf",
		AdminIPs:          "192.0.2.1",
		Security:          SecurityConfig{Enabled: true, Threshold: 3},
		Groups: map[string]GroupConfig{
			"edge": {
				FirewallAllowlist: "cf",
				AdminIPs:          "192.0.2.2",
				Security:          &SecurityConfig{Enabled: true, Threshold: 9},
				Tags:              []string{"dmz"},
			},
		},
	}
	host := ProvisionHost{Name: "a", Host: "192.0.2.1", Group: "edge", AdminIPs: "192.0.2.3", SwapSizeMB: 2048}
	r := resolveHostConfig(pf, host)
	if r.adminIPs != "192.0.2.3" {
		t.Errorf("host override lost: %q", r.adminIPs)
	}
	if r.security.Threshold != 9 {
		t.Errorf("group security not inherited: %d", r.security.Threshold)
	}
	if r.swap.SizeMB != 2048 {
		t.Errorf("host swap override lost: %d", r.swap.SizeMB)
	}
	if len(r.tags) != 1 || r.tags[0] != "dmz" {
		t.Errorf("group tags not inherited: %v", r.tags)
	}

	plain := ProvisionHost{Name: "b", Host: "2001:db8::1"}
	r2 := resolveHostConfig(pf, plain)
	if r2.adminIPs != "192.0.2.1" {
		t.Errorf("global default lost: %q", r2.adminIPs)
	}
	if r2.security.Threshold != 3 {
		t.Errorf("global security not applied: %d", r2.security.Threshold)
	}
}

func TestSelectHostsByTags(t *testing.T) {
	hosts := []ProvisionHost{
		{Name: "a", Tags: []string{"dmz"}},
		{Name: "b", Tags: []string{"internal"}},
		{Name: "c"},
	}
	if got := selectHostsByTags(hosts, "dmz"); len(got) != 1 || got[0].Name != "a" {
		t.Errorf("dmz filter: %+v", got)
	}
	if got := selectHostsByTags(hosts, ""); len(got) != 3 {
		t.Errorf("empty filter should return all")
	}
	if got := selectHostsByTags(hosts, "dmz,internal"); len(got) != 2 {
		t.Errorf("multi-tag filter: %+v", got)
	}
	if got := selectHostsByTags(hosts, "nope"); len(got) != 0 {
		t.Errorf("no match should be empty")
	}
}

func TestValidateVLANs(t *testing.T) {
	hosts := []ProvisionHost{
		{Name: "a", Host: "192.0.2.1"},
		{Name: "b", Host: "192.0.2.2"},
	}
	ok := &ProvisionFile{
		Mode:  "docker",
		Hosts: hosts,
		VLANs: []ProvisionVLAN{
			{Name: "internal", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{
				{Name: "a", Iface: "eth1", IP: "198.51.100.1"},
				{Name: "b", Iface: "eth1", IP: "198.51.100.2"},
			}},
		},
	}
	if _, err := validateProvision(ok); err != nil {
		t.Fatalf("valid vlan rejected: %v", err)
	}
	if got := vlansForHost(*ok, "b"); len(got) != 1 || got[0].Name != "internal" {
		t.Errorf("vlansForHost(b) = %+v", got)
	}
	if got := vlansForHost(*ok, "ghost"); len(got) != 0 {
		t.Errorf("vlansForHost(ghost) should be empty")
	}

	bad := []*ProvisionFile{
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "v", CIDR: "not-a-cidr", Hosts: []VLANHostAssign{{Name: "a", Iface: "eth1", IP: "198.51.100.1"}}}}},
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "v", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{{Name: "ghost", Iface: "eth1", IP: "198.51.100.1"}}}}},
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "v", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{{Name: "a", Iface: "", IP: "198.51.100.1"}}}}},
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "v", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{{Name: "a", Iface: "eth1", IP: "203.0.113.5"}}}}},
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "v", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{{Name: "a", Iface: "eth1", IP: "nope"}}}}},
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "v", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{{Name: "a", Iface: "eth1", IP: "198.51.100.1"}, {Name: "b", Iface: "eth1", IP: "198.51.100.1"}}}}},
		{Mode: "docker", Hosts: hosts, VLANs: []ProvisionVLAN{{Name: "", CIDR: "198.51.100.0/24", Hosts: []VLANHostAssign{{Name: "a", Iface: "eth1", IP: "198.51.100.1"}}}}},
	}
	for i, pf := range bad {
		if _, err := validateProvision(pf); err == nil {
			t.Errorf("vlan case %d: expected validation error", i)
		}
	}
}

func TestParseAdminIPs(t *testing.T) {
	admin4, admin6, err := parseAdminIPs("203.0.113.10,2001:db8::1,2001:db8::/32")
	if err != nil {
		t.Fatalf("parseAdminIPs: %v", err)
	}
	if len(admin4) != 1 || admin4[0] != "203.0.113.10/32" {
		t.Errorf("admin4 = %v", admin4)
	}
	if len(admin6) != 2 {
		t.Errorf("admin6 = %v", admin6)
	}
	if _, _, err := parseAdminIPs("not-an-ip"); err == nil {
		t.Error("invalid IP should error")
	}
	if a4, a6, err := parseAdminIPs(""); err != nil || len(a4)+len(a6) != 0 {
		t.Error("empty should return no entries")
	}
}

func TestParseAdminIPsRejectsBad(t *testing.T) {
	if _, _, err := parseAdminIPs("  , ,"); err != nil {
		t.Errorf("whitespace-only should be tolerated: %v", err)
	}
	admin4, admin6, err := parseAdminIPs(strings.TrimSpace(" 203.0.113.10 , 2001:db8::1 "))
	if err != nil {
		t.Fatalf("trimmed parse: %v", err)
	}
	if len(admin4) != 1 || len(admin6) != 1 {
		t.Errorf("want 1v4 + 1v6, got %v / %v", admin4, admin6)
	}
}
