package providers

import (
	"testing"
)

func TestVPSCreateConfigDefaults(t *testing.T) {
	cfg := VPSCreateConfig{
		Plan:     "gp.nano",
		Location: "us-mia-1",
		Template: "ubuntu-24",
	}
	if cfg.Plan != "gp.nano" {
		t.Errorf("Plan = %q, want %q", cfg.Plan, "gp.nano")
	}
	if cfg.Location != "us-mia-1" {
		t.Errorf("Location = %q, want %q", cfg.Location, "us-mia-1")
	}
	if cfg.Template != "ubuntu-24" {
		t.Errorf("Template = %q, want %q", cfg.Template, "ubuntu-24")
	}
}

func TestVPSStruct(t *testing.T) {
	vps := VPS{
		ID:     "123",
		Name:   "test-vps",
		IP:     "1.2.3.4",
		Status: "active",
		Plan:   "gp.nano",
	}
	if vps.ID != "123" {
		t.Errorf("ID = %q, want %q", vps.ID, "123")
	}
	if vps.Name != "test-vps" {
		t.Errorf("Name = %q, want %q", vps.Name, "test-vps")
	}
	if vps.IP != "1.2.3.4" {
		t.Errorf("IP = %q, want %q", vps.IP, "1.2.3.4")
	}
}

func TestK8sClusterStruct(t *testing.T) {
	c := K8sCluster{
		ID:        "cluster-1",
		Name:      "prod-cluster",
		Status:    "running",
		NodeCount: 3,
		Version:   "1.30",
	}
	if c.NodeCount != 3 {
		t.Errorf("NodeCount = %d, want %d", c.NodeCount, 3)
	}
	if c.Version != "1.30" {
		t.Errorf("Version = %q, want %q", c.Version, "1.30")
	}
}

func TestDNSRecordStruct(t *testing.T) {
	r := DNSRecord{
		Type:  "A",
		Name:  "www",
		Value: "1.2.3.4",
		TTL:   300,
	}
	if r.Type != "A" {
		t.Errorf("Type = %q, want %q", r.Type, "A")
	}
	if r.Name != "www" {
		t.Errorf("Name = %q, want %q", r.Name, "www")
	}
	if r.Value != "1.2.3.4" {
		t.Errorf("Value = %q, want %q", r.Value, "1.2.3.4")
	}
}

func TestLoadBalancerStruct(t *testing.T) {
	lb := LoadBalancer{
		ID:     "lb-1",
		Name:   "my-lb",
		IP:     "5.6.7.8",
		Status: "active",
	}
	if lb.IP != "5.6.7.8" {
		t.Errorf("IP = %q, want %q", lb.IP, "5.6.7.8")
	}
}

func TestDNSZoneStruct(t *testing.T) {
	z := DNSZone{
		ID:   "zone-1",
		Name: "example.com",
	}
	if z.Name != "example.com" {
		t.Errorf("Name = %q, want %q", z.Name, "example.com")
	}
}

func TestBareMetalStruct(t *testing.T) {
	bm := BareMetal{
		ID:     "bm-1",
		Name:   "bare-1",
		IP:     "10.0.0.1",
		Status: "active",
	}
	if bm.Name != "bare-1" {
		t.Errorf("Name = %q, want %q", bm.Name, "bare-1")
	}
}
