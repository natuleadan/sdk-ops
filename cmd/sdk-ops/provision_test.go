package main

import (
	"strings"
	"testing"
)

func TestValidateProvision(t *testing.T) {
	ok := &ProvisionFile{
		Mode: "docker",
		Hosts: []ProvisionHost{
			{Name: "a", Host: "1.1.1.1"},
			{Name: "b", Host: "2a03::1"},
		},
		Peers: []ProvisionPeer{
			{From: "a", To: "b", Ports: []int{43453}},
		},
	}
	names, err := validateProvision(ok)
	if err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	if names["b"] != "2a03::1" {
		t.Errorf("names map wrong: %v", names)
	}

	bad := []*ProvisionFile{
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a"}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "1.1.1.1"}}, Peers: []ProvisionPeer{{From: "x", To: "a", Ports: []int{1}}}},
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "1.1.1.1"}}, Peers: []ProvisionPeer{{From: "a", To: "a"}}},
	}
	for i, pf := range bad {
		if _, err := validateProvision(pf); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestParseAdminIPs(t *testing.T) {
	admin4, admin6, err := parseAdminIPs("181.199.62.245,2a03:4000::1,2001:db8::/32")
	if err != nil {
		t.Fatalf("parseAdminIPs: %v", err)
	}
	if len(admin4) != 1 || admin4[0] != "181.199.62.245/32" {
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
	admin4, admin6, err := parseAdminIPs(strings.TrimSpace(" 181.199.62.245 , 2a03::1 "))
	if err != nil {
		t.Fatalf("trimmed parse: %v", err)
	}
	if len(admin4) != 1 || len(admin6) != 1 {
		t.Errorf("want 1v4 + 1v6, got %v / %v", admin4, admin6)
	}
}
