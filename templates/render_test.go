package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProfiles(t *testing.T) {
	p, err := LoadProfiles("nats-dockerized")
	if err != nil {
		t.Fatal(err)
	}
	if p["lite"]["max_connections"] != 100 {
		t.Errorf("lite max_connections = %v, want 100", p["lite"]["max_connections"])
	}
	if p["rs"]["max_file_store"] != "10GB" {
		t.Errorf("rs max_file_store = %v, want 10GB", p["rs"]["max_file_store"])
	}
}

func TestRenderDirNATS(t *testing.T) {
	data := map[string]any{
		"ServerName":      "node-a",
		"Advertise":       "203.0.113.10",
		"Routes":          []string{"203.0.113.11", "2001:db8::2"},
		"ClusterName":     "nla",
		"MaxConnections":  100,
		"MaxFileStore":    "2GB",
		"MaxMemoryStore":  "128MB",
		"MemLimit":        "1g",
		"Cpus":            1,
		"JSKey":           "sekret",
		"AppPasswordHash": "$2a$11$xxx",
		"SvcPasswordHash": "$2a$11$yyy",
		"SysPasswordHash": "$2a$11$zzz",
		"ServerTags":        `["region:mia"]`,
		"ClientAdvertise":   "203.0.113.10:4222",
		"AppPublishAllow":   `["demo","demo.>","events.>","$KV.>","myapp.>"]`,
		"AppSubscribeAllow": `["demo.>","events.>","$KV.>","myapp.reply.>"]`,
	}
	dir := t.TempDir()
	if err := RenderDir("nats-dockerized", dir, data); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "nats.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"server_name: node-a",
		"advertise: 203.0.113.10:6222",
		"nats://203.0.113.11:6222",
		"nats://2001:db8::2:6222",
		"max_connections: 100",
		"max_file_store: 2GB",
		"max_memory_store: 128MB",
		`server_tags: ["region:mia"]`,
		"client_advertise: 203.0.113.10:4222",
		`allow: ["demo","demo.>","events.>","$KV.>","myapp.>"]`,
		`allow: ["demo.>","events.>","$KV.>","myapp.reply.>"]`,
	} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("nats.conf missing %q", want)
		}
	}
	for _, f := range []string{"service.yaml", "docker-compose.yml", "backup.sh", "restore.sh", "validate.sh"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not rendered: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles.yaml")); err == nil {
		t.Error("profiles.yaml should be excluded from the render")
	}
}
