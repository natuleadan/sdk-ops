package main

import (
	"testing"
)

func TestScriptNeedsUpdate(t *testing.T) {
	tests := []struct {
		name     string
		remote   string
		template string
		want     bool
	}{
		{"identical", "#!/bin/bash\nlog ok\n", "#!/bin/bash\nlog ok\n", false},
		{"trailing newline diff", "a\nb\n", "a\nb", false},
		{"content differs", "a\n", "b\n", true},
		{"remote empty", "", "b\n", true},
	}
	for _, tt := range tests {
		if got := scriptNeedsUpdate(tt.remote, tt.template); got != tt.want {
			t.Errorf("%s: scriptNeedsUpdate = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseOpsComponents(t *testing.T) {
	all, err := parseOpsComponents("")
	if err != nil {
		t.Fatalf("empty components: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("default components = %d, want 5 (%v)", len(all), all)
	}
	got, err := parseOpsComponents("state,traefik")
	if err != nil {
		t.Fatalf("valid components: %v", err)
	}
	if len(got) != 2 || got[0] != "state" || got[1] != "traefik" {
		t.Errorf("components parsed wrong: %v", got)
	}
	if _, err := parseOpsComponents("state,nope"); err == nil {
		t.Error("unknown component must fail")
	}
}

func TestOpsTargets(t *testing.T) {
	pf := &ProvisionFile{Hosts: []ProvisionHost{{Name: "a", Host: "1.1.1.1"}, {Name: "b", Host: "2a03::1"}}}
	hosts, pfOut, err := opsTargets(pf, "", infraFlags{})
	if err != nil || len(hosts) != 2 || pfOut != pf {
		t.Errorf("fleet targets wrong: %v %v", err, hosts)
	}
	hosts, _, err = opsTargets(pf, "2a03::1", infraFlags{})
	if err != nil || len(hosts) != 1 || hosts[0].Name != "b" {
		t.Errorf("filtered targets wrong: %v %v", err, hosts)
	}
	if _, _, err := opsTargets(pf, "9.9.9.9", infraFlags{}); err == nil {
		t.Error("unknown node must fail")
	}
	hosts, pfOut, err = opsTargets(nil, "1.1.1.1", infraFlags{user: "sdkops"})
	if err != nil || pfOut != nil || len(hosts) != 1 || hosts[0].User != "sdkops" {
		t.Errorf("single node targets wrong: %v %v", err, hosts)
	}
	if _, _, err := opsTargets(nil, "", infraFlags{}); err == nil {
		t.Error("no yaml and no node must fail")
	}
}

func TestOpsComponentTable(t *testing.T) {
	table := opsComponents()
	if len(table) != 5 {
		t.Fatalf("component table size = %d, want 5", len(table))
	}
	for name, c := range table {
		if c.template() == "" {
			t.Errorf("component %s: empty template", name)
		}
		if c.scriptPath == "" {
			t.Errorf("component %s: empty script path", name)
		}
		if c.install == nil || c.remove == nil {
			t.Errorf("component %s: missing install/remove", name)
		}
		if name != "logrotate" && (c.timerName == "" || c.serviceName == "" || len(c.units) == 0) {
			t.Errorf("component %s: missing systemd wiring", name)
		}
	}
	// allowlist must require the fleet YAML (admin IPs)
	if !table["allowlist"].requiresYAML {
		t.Error("allowlist must require --provision-yaml")
	}
	if table["logrotate"].timerName != "" {
		t.Error("logrotate must not declare a timer")
	}
}
