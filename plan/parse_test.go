package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFile_ValidYAML(t *testing.T) {
	p, err := ParseFile("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if p.Mode != "k3s" {
		t.Errorf("Mode = %q, want %q", p.Mode, "k3s")
	}
	if p.Parallel != 3 {
		t.Errorf("Parallel = %d, want %d", p.Parallel, 3)
	}
	if len(p.Hosts) != 2 {
		t.Fatalf("len(Hosts) = %d, want %d", len(p.Hosts), 2)
	}
	if p.Hosts[0].Role != "server" {
		t.Errorf("Hosts[0].Role = %q, want %q", p.Hosts[0].Role, "server")
	}
	if p.Hosts[0].Host != "192.168.1.10" {
		t.Errorf("Hosts[0].Host = %q, want %q", p.Hosts[0].Host, "192.168.1.10")
	}
	if p.Hosts[0].Name != "server-1" {
		t.Errorf("Hosts[0].Name = %q, want %q", p.Hosts[0].Name, "server-1")
	}
	if p.Hosts[1].Role != "agent" {
		t.Errorf("Hosts[1].Role = %q, want %q", p.Hosts[1].Role, "agent")
	}
}

func TestParseFile_ValidJSON(t *testing.T) {
	p, err := ParseFile("testdata/valid.json")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if p.Mode != "k3s" {
		t.Errorf("Mode = %q, want %q", p.Mode, "k3s")
	}
	if len(p.Hosts) != 1 {
		t.Fatalf("len(Hosts) = %d, want %d", len(p.Hosts), 1)
	}
	if p.Hosts[0].Role != "server" {
		t.Errorf("Hosts[0].Role = %q, want %q", p.Hosts[0].Role, "server")
	}
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile("testdata/nonexistent.yaml")
	if err == nil {
		t.Fatal("ParseFile() expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "no such file") {
		t.Errorf("error = %v, want 'no such file'", err)
	}
}

func TestParseFile_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	os.WriteFile(path, []byte("{{invalid yaml"), 0644)

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("ParseFile() expected error for invalid YAML")
	}
}

func TestValidate_EmptyHosts(t *testing.T) {
	p := &Plan{Hosts: []Host{}}
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for empty hosts")
	}
	if !strings.Contains(err.Error(), "at least one host") {
		t.Errorf("Validate() error = %q, want 'at least one host'", err)
	}
}

func TestValidate_NoServers(t *testing.T) {
	p := &Plan{Hosts: []Host{
		{Name: "a1", Role: "agent", Host: "1.2.3.4"},
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for no servers")
	}
	if !strings.Contains(err.Error(), "at least one server") {
		t.Errorf("Validate() error = %q, want 'at least one server'", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	p := &Plan{Hosts: []Host{
		{Role: "server", Host: "1.2.3.4"},
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for missing name")
	}
}

func TestValidate_MissingHost(t *testing.T) {
	p := &Plan{Hosts: []Host{
		{Name: "s1", Role: "server"},
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for missing host")
	}
	if !strings.Contains(err.Error(), "host (IP) is required") {
		t.Errorf("Validate() error = %q, want 'host (IP) is required'", err)
	}
}

func TestValidate_InvalidRole(t *testing.T) {
	p := &Plan{Hosts: []Host{
		{Name: "s1", Role: "master", Host: "1.2.3.4"},
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for invalid role")
	}
	if !strings.Contains(err.Error(), "role must be") {
		t.Errorf("Validate() error = %q, want 'role must be'", err)
	}
}

func TestValidate_Valid(t *testing.T) {
	p := &Plan{Hosts: []Host{
		{Name: "s1", Role: "server", Host: "1.2.3.4"},
		{Name: "a1", Role: "agent", Host: "1.2.3.5"},
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestFillDefaults(t *testing.T) {
	p := &Plan{}
	p.fillDefaults()
	if p.Mode != "k3s" {
		t.Errorf("Mode = %q, want %q", p.Mode, "k3s")
	}
	if p.Parallel != 5 {
		t.Errorf("Parallel = %d, want %d", p.Parallel, 5)
	}
	if p.ServerOptions.User != "root" {
		t.Errorf("ServerOptions.User = %q, want %q", p.ServerOptions.User, "root")
	}
	if p.ServerOptions.SSHPort != 22 {
		t.Errorf("ServerOptions.SSHPort = %d, want %d", p.ServerOptions.SSHPort, 22)
	}
	if p.ServerOptions.K3sChannel != "stable" {
		t.Errorf("ServerOptions.K3sChannel = %q, want %q", p.ServerOptions.K3sChannel, "stable")
	}
	if p.AgentOptions.User != "root" {
		t.Errorf("AgentOptions.User = %q, want %q", p.AgentOptions.User, "root")
	}
}

func TestFillDefaults_HostOverrides(t *testing.T) {
	p := &Plan{
		ServerOptions: Options{User: "ubuntu", SSHPort: 2222},
		AgentOptions:  Options{User: "admin", SSHPort: 2223},
		Hosts: []Host{
			{Name: "s1", Role: "server", Host: "1.1.1.1"},
			{Name: "a1", Role: "agent", Host: "2.2.2.2"},
		},
	}
	p.fillDefaults()
	if p.Hosts[0].User != "ubuntu" {
		t.Errorf("Hosts[0].User = %q, want %q", p.Hosts[0].User, "ubuntu")
	}
	if p.Hosts[0].Port != 2222 {
		t.Errorf("Hosts[0].Port = %d, want %d", p.Hosts[0].Port, 2222)
	}
	if p.Hosts[1].User != "admin" {
		t.Errorf("Hosts[1].User = %q, want %q", p.Hosts[1].User, "admin")
	}
	if p.Hosts[1].Port != 2223 {
		t.Errorf("Hosts[1].Port = %d, want %d", p.Hosts[1].Port, 2223)
	}
}

func TestSummary(t *testing.T) {
	p := &Plan{
		Mode:     "k3s",
		Parallel: 3,
		Hosts: []Host{
			{Name: "s1", Role: "server", Host: "1.1.1.1"},
			{Name: "a1", Role: "agent", Host: "2.2.2.2"},
			{Name: "a2", Role: "agent", Host: "3.3.3.3"},
		},
	}
	s := p.Summary()
	if !strings.Contains(s, "k3s") {
		t.Errorf("Summary missing mode")
	}
	if !strings.Contains(s, "s1 (1.1.1.1)") {
		t.Errorf("Summary missing server s1")
	}
	if !strings.Contains(s, "a1 (2.2.2.2)") {
		t.Errorf("Summary missing agent a1")
	}
	if !strings.Contains(s, "a2 (3.3.3.3)") {
		t.Errorf("Summary missing agent a2")
	}
}
