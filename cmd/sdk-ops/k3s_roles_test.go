package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestK3sServerArgs(t *testing.T) {
	h := ProvisionHost{Name: "cp1", Host: "192.0.2.10", PeerIP: "192.0.2.20"}
	pf := ProvisionFile{Mode: "k3s", K3sIface: "ens19"}
	got := k3sServerArgs(h, pf, true)
	for _, want := range []string{"--cluster-init", "--node-ip 192.0.2.20", "--advertise-address 192.0.2.20", "--flannel-iface ens19", "--tls-san 192.0.2.10"} {
		if !strings.Contains(got, want) {
			t.Errorf("k3sServerArgs missing %q in %q", want, got)
		}
	}
	if plain := k3sServerArgs(h, pf, false); strings.Contains(plain, "--cluster-init") {
		t.Errorf("non-HA server args must not cluster-init: %q", plain)
	}
}

func k3sFleet(roles ...string) *ProvisionFile {
	pf := &ProvisionFile{Mode: "k3s"}
	hosts := []ProvisionHost{
		{Name: "node1", Host: "192.0.2.1"},
		{Name: "node2", Host: "192.0.2.2"},
		{Name: "node3", Host: "192.0.2.3"},
	}
	for i, r := range roles {
		if i < len(hosts) {
			hosts[i].Role = r
		}
	}
	pf.Hosts = hosts
	return pf
}

func TestValidateK3sRoles(t *testing.T) {
	ok := []*ProvisionFile{
		k3sFleet("", "", ""),
		k3sFleet("server", "agent", "agent"),
		k3sFleet("server", "", ""), // roleless hosts default to agent
	}
	ok[2].K3sHA = true
	ok = append(ok, k3sFleet("server", "server", "server"))
	ok[len(ok)-1].K3sHA = true
	ok = append(ok, k3sFleet("server", "server", "agent")) // HA control plane + workers
	ok[len(ok)-1].K3sHA = true
	for i, pf := range ok {
		if _, err := validateProvision(pf); err != nil {
			t.Errorf("valid case %d rejected: %v", i, err)
		}
	}

	bad := []*ProvisionFile{
		{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1", Role: "server"}}},
		k3sFleet("agent", "agent", "agent"), // no server
		k3sFleet("server", "server", ""),    // 2 servers without k3s_ha
		k3sFleet("boss", "", ""),            // invalid role
	}
	for i, pf := range bad {
		if _, err := validateProvision(pf); err == nil {
			t.Errorf("invalid case %d accepted", i)
		}
	}
}

func TestK3sRoleOf(t *testing.T) {
	pf := k3sFleet("server", "", "agent")
	if got := k3sRoleOf(*pf, "node1"); got != "server" {
		t.Errorf("node1 role = %q, want server", got)
	}
	if got := k3sRoleOf(*pf, "node2"); got != "agent" {
		t.Errorf("node2 (roleless) = %q, want agent", got)
	}
	if got := k3sRoleOf(*pf, "node3"); got != "agent" {
		t.Errorf("node3 role = %q, want agent", got)
	}
	if got := k3sRoleOf(*pf, "ghost"); got != "" {
		t.Errorf("ghost role = %q, want empty", got)
	}
	legacy := k3sFleet("", "", "")
	if got := k3sRoleOf(*legacy, "node1"); got != "" {
		t.Errorf("legacy role = %q, want empty (no roles declared)", got)
	}
	docker := &ProvisionFile{Mode: "docker", Hosts: []ProvisionHost{{Name: "a", Host: "192.0.2.1"}}}
	if got := k3sRoleOf(*docker, "a"); got != "" {
		t.Errorf("docker mode role = %q, want empty", got)
	}
}

func TestK3sFirstServerHost(t *testing.T) {
	pf := k3sFleet("agent", "server", "agent")
	if first := k3sFirstServerHost(*pf); first == nil || first.Name != "node2" {
		t.Fatalf("roles fleet first server = %v, want node2", first)
	}
	ha := k3sFleet("", "", "")
	ha.K3sHA = true
	if first := k3sFirstServerHost(*ha); first == nil || first.Name != "node1" {
		t.Fatalf("legacy HA first server = %v, want node1", first)
	}
	none := k3sFleet("", "", "")
	if first := k3sFirstServerHost(*none); first != nil {
		t.Fatalf("legacy non-HA first server = %v, want nil", first)
	}
}

func TestRolesYAML(t *testing.T) {
	y := `
mode: k3s
k3s_ha: true
k3s_disable_traefik: false
hosts:
  - name: cp1
    host: 192.0.2.10
    role: server
  - name: w1
    host: 192.0.2.11
    role: agent
`
	var pf ProvisionFile
	if err := yaml.Unmarshal([]byte(y), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.Hosts[0].Role != "server" || pf.Hosts[1].Role != "agent" {
		t.Fatalf("roles not parsed: %+v", pf.Hosts)
	}
	if pf.K3sDisableTraefik {
		t.Fatal("k3s_disable_traefik should be false")
	}
	if _, err := validateProvision(&pf); err != nil {
		t.Fatalf("roles yaml rejected: %v", err)
	}
}
