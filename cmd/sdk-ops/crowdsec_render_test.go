package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/natuleadan/sdk-ops/templates"
)

func TestCrowdsecClusterRenderData(t *testing.T) {
	prof := map[string]any{
		"lapi_cpu": "100m", "lapi_cpu_limit": "500m",
		"lapi_mem": "96Mi", "lapi_mem_limit": "256Mi",
		"lapi_storage": "1Gi",
		"agent_cpu":    "100m", "agent_cpu_limit": "300m",
		"agent_mem": "64Mi", "agent_mem_limit": "128Mi",
		"appsec": "true",
	}
	data, err := crowdsecClusterRenderData(prof)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if data["Namespace"] != "crowdsec" || data["Release"] != "crowdsec" {
		t.Errorf("namespace/release defaults wrong: %v / %v", data["Namespace"], data["Release"])
	}
	if data["Bouncer"] != "traefik-bouncer" {
		t.Errorf("bouncer default = %v", data["Bouncer"])
	}
	if data["LapiCPU"] != "100m" || data["AgentMemLimit"] != "128Mi" {
		t.Errorf("profile mapping wrong: %v / %v", data["LapiCPU"], data["AgentMemLimit"])
	}
	if enabled, ok := data["AppSecEnabled"].(bool); !ok || !enabled {
		t.Errorf("appsec=true must render AppSecEnabled=true, got %v", data["AppSecEnabled"])
	}
	if data["PodCIDR"] != "10.42.0.0/16" { // go-check:ignore-ip
		t.Errorf("pod cidr default = %v", data["PodCIDR"])
	}
	if cidrs, ok := data["TrustedCIDRs"].([]string); !ok || len(cidrs) != 2 {
		t.Errorf("trusted cidrs default = %v", data["TrustedCIDRs"])
	}

	prof["appsec"] = "false"
	data, err = crowdsecClusterRenderData(prof)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if enabled, ok := data["AppSecEnabled"].(bool); !ok || enabled {
		t.Errorf("appsec=false must render AppSecEnabled=false, got %v", data["AppSecEnabled"])
	}
}

func TestCrowdsecClusterUninstallAndOrder(t *testing.T) {
	u, ok := serviceUninstalls["crowdsec-cluster"]
	if !ok {
		t.Fatal("crowdsec-cluster must declare a serviceUninstall")
	}
	joined := ""
	for _, c := range u.script {
		joined += c + "\n"
	}
	for _, want := range []string{"helmchartconfig traefik", "helm uninstall crowdsec", "delete ns crowdsec"} {
		if !strings.Contains(joined, want) {
			t.Errorf("crowdsec uninstall missing %q", want)
		}
	}

	svc := ProvisionServices{"crowdsec-cluster": {Profile: "lite"}, "nats-cluster": {Profile: "lite"}}
	ordered := orderedServiceNames(svc)
	if len(ordered) != 2 || ordered[0] != "nats-cluster" || ordered[1] != "crowdsec-cluster" {
		t.Errorf("order wrong: %v", ordered)
	}
}

// crowdsecBareProf is the profile fixture for the bare-template tests.
func crowdsecBareProf() map[string]any {
	return map[string]any{
		"mem_limit":   "256M",
		"cpu_quota":   "50%",
		"collections": "crowdsecurity/linux crowdsecurity/sshd",
	}
}

// TestCrowdsecBareStandalone locks the standalone defaults (no CS_LAPI_URL).
func TestCrowdsecBareStandalone(t *testing.T) {
	h := ProvisionHost{Name: "mia-02"}
	data, err := crowdsecBareRenderData(ProvisionFile{}, h, crowdsecBareProf(), ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if client, _ := data["Client"].(bool); client {
		t.Error("standalone by default: Client must be false without CS_LAPI_URL")
	}
	if data["BouncerName"] != "mia-02" || data["CSVersion"] != "1.8.1" || data["BouncerVersion"] != "0.0.36" {
		t.Errorf("defaults wrong: bouncer=%v cs=%v bouncer_ver=%v", data["BouncerName"], data["CSVersion"], data["BouncerVersion"])
	}
	if data["MemLimit"] != "256M" || data["CpuQuota"] != "50%" {
		t.Errorf("profile mapping wrong: mem=%v cpu=%v", data["MemLimit"], data["CpuQuota"])
	}
	if data["LapiListen"] != "127.0.0.1:8080" {
		t.Errorf("lapi listen default = %v", data["LapiListen"])
	}
}

// TestCrowdsecBareClientMode asserts CS_LAPI_URL switches to client mode and
// that the rendered init.sh carries the client flag (a template regression
// would otherwise only fail at deploy time).
func TestCrowdsecBareClientMode(t *testing.T) {
	t.Setenv("CS_LAPI_URL", "http://192.0.2.10:30080")
	t.Setenv("CS_VERSION", "1.8.2")
	data, err := crowdsecBareRenderData(ProvisionFile{}, ProvisionHost{Name: "mia-02"}, crowdsecBareProf(), ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render client: %v", err)
	}
	if client, _ := data["Client"].(bool); !client {
		t.Error("CS_LAPI_URL must switch to client mode")
	}
	if data["LapiURL"] != "http://192.0.2.10:30080" || data["CSVersion"] != "1.8.2" {
		t.Errorf("client render wrong: url=%v cs=%v", data["LapiURL"], data["CSVersion"])
	}

	dir := t.TempDir()
	if err := templates.RenderDir("crowdsec-bare", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	initSh, err := os.ReadFile(filepath.Join(dir, "init.sh"))
	if err != nil {
		t.Fatalf("read rendered init.sh: %v", err)
	}
	if !strings.Contains(string(initSh), `CLIENT="1"`) {
		t.Errorf("client mode not baked into init.sh:\n%s", string(initSh)[:200])
	}
}

// TestCrowdsecDockerizedRenderData locks the standalone/client switch, the
// image/plugin pins and that the compose renders with the chosen tag.
func TestCrowdsecDockerizedRenderData(t *testing.T) {
	prof := map[string]any{"mem_limit": "256M", "cpus": "0.5", "collections": "crowdsecurity/linux"}
	h := ProvisionHost{Name: "web"}
	data, err := crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if client, _ := data["Client"].(bool); client {
		t.Error("standalone by default: Client must be false without CS_LAPI_URL")
	}
	if data["ImageTag"] != "v1.8.1" || data["PluginVersion"] != "v1.7.1" {
		t.Errorf("pins wrong: image=%v plugin=%v", data["ImageTag"], data["PluginVersion"])
	}
	if data["MemLimit"] != "256M" || data["Cpus"] != "0.5" {
		t.Errorf("profile mapping wrong: mem=%v cpus=%v", data["MemLimit"], data["Cpus"])
	}
	if cidrs, ok := data["TrustedCIDRs"].([]string); !ok || len(cidrs) != 4 {
		t.Errorf("trusted cidrs default = %v", data["TrustedCIDRs"])
	}

	t.Setenv("CS_LAPI_URL", "http://192.0.2.20:30080")
	data, err = crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render client: %v", err)
	}
	if client, _ := data["Client"].(bool); !client {
		t.Error("CS_LAPI_URL must switch to client mode")
	}

	dir := t.TempDir()
	if err := templates.RenderDir("crowdsec-dockerized", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read rendered compose: %v", err)
	}
	if !strings.Contains(string(compose), "crowdsecurity/crowdsec:v1.8.1") {
		t.Errorf("image pin not rendered:\n%s", string(compose))
	}
}

// TestCrowdsecDockerizedClientRuntime locks client mode being runtime-driven:
// the provision writes per-host CS_LAPI_URL_* into .env, which the render
// cannot see, so init.sh derives CLIENT from the environment at runtime.
func TestCrowdsecDockerizedClientRuntime(t *testing.T) {
	prof := map[string]any{"mem_limit": "256M", "cpus": "0.5", "collections": "crowdsecurity/linux"}
	h := ProvisionHost{Name: "web"}
	t.Setenv("CS_LAPI_URL", "http://192.0.2.20:30080")
	data, err := crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render client: %v", err)
	}
	dir := t.TempDir()
	if err := templates.RenderDir("crowdsec-dockerized", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	initSh, err := os.ReadFile(filepath.Join(dir, "init.sh"))
	if err != nil {
		t.Fatalf("read rendered init.sh: %v", err)
	}
	if !strings.Contains(string(initSh), `[ -n "${CS_LAPI_URL:-}" ] && CLIENT="1"`) {
		t.Error("client mode must be runtime-driven from CS_LAPI_URL (per-host .env)")
	}
}

// TestCrowdsecDockerizedCentralPublish locks the central mode: with
// central:true and a peer IP the compose publishes the LAPI on the VLAN for
// remote clients; otherwise only loopback is published.
func TestCrowdsecDockerizedCentralPublish(t *testing.T) {
	prof := map[string]any{"mem_limit": "256M", "cpus": "0.5", "collections": "crowdsecurity/linux"}
	h := ProvisionHost{Name: "waf-01", PeerIP: "192.0.2.10"}
	data, err := crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "lite", Central: true})
	if err != nil {
		t.Fatalf("render central: %v", err)
	}
	if central, _ := data["Central"].(bool); !central {
		t.Error("central:true must reach the render context")
	}
	dir := t.TempDir()
	if err := templates.RenderDir("crowdsec-dockerized", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read rendered compose: %v", err)
	}
	if !strings.Contains(string(compose), `"192.0.2.10:8080:8080"`) {
		t.Errorf("central must publish the LAPI on the peer IP:\n%s", string(compose))
	}

	data, err = crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render standalone: %v", err)
	}
	dir = t.TempDir()
	if err := templates.RenderDir("crowdsec-dockerized", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	compose, err = os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read rendered compose: %v", err)
	}
	if strings.Contains(string(compose), "192.0.2.10:8080") {
		t.Error("standalone must not publish the LAPI on the peer IP")
	}
}

// TestCrowdsecDockerizedAppSecRender locks the profile-gated AppSec WAF:
// normal enables the :7422 server (acquis mount, installs, plugin options,
// validate check), lite stays stream-only.
func TestCrowdsecDockerizedAppSecRender(t *testing.T) {
	prof := map[string]any{"mem_limit": "512M", "cpus": "1", "collections": "crowdsecurity/linux", "appsec": "true"}
	h := ProvisionHost{Name: "web"}
	data, err := crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "normal"})
	if err != nil {
		t.Fatalf("render normal: %v", err)
	}
	if appsec, _ := data["AppSec"].(bool); !appsec {
		t.Fatal("normal profile must enable AppSec")
	}
	dir := t.TempDir()
	if err := templates.RenderDir("crowdsec-dockerized", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	compose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if !strings.Contains(string(compose), "acquis-appsec.yaml") {
		t.Error("normal compose must mount the appsec acquisition")
	}
	initSh, _ := os.ReadFile(filepath.Join(dir, "init.sh"))
	if !strings.Contains(string(initSh), "crowdsecAppsecHost: crowdsec:7422") {
		t.Error("normal init must wire the plugin to the appsec server")
	}
	if !strings.Contains(string(initSh), "crs-inband") {
		t.Error("normal init must install the in-band CRS config")
	}
	validateSh, _ := os.ReadFile(filepath.Join(dir, "validate.sh"))
	if !strings.Contains(string(validateSh), ":7422") {
		t.Error("normal validate must check the appsec listener")
	}

	prof["appsec"] = "false"
	data, err = crowdsecDockerizedRenderData(ProvisionFile{}, h, prof, ServiceConfig{Profile: "lite"})
	if err != nil {
		t.Fatalf("render lite: %v", err)
	}
	if appsec, _ := data["AppSec"].(bool); appsec {
		t.Fatal("lite profile must stay stream-only")
	}
	dir = t.TempDir()
	if err := templates.RenderDir("crowdsec-dockerized", dir, data); err != nil {
		t.Fatalf("render dir: %v", err)
	}
	compose, _ = os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if strings.Contains(string(compose), "acquis-appsec.yaml") {
		t.Error("lite compose must not mount the appsec acquisition")
	}
	initSh, _ = os.ReadFile(filepath.Join(dir, "init.sh"))
	if strings.Contains(string(initSh), "crowdsecAppsecEnabled") {
		t.Error("lite init must not wire appsec plugin options")
	}
}

// TestCrowdsecBareUninstallAndOrder keeps the bare service in the declared
// cleanup map (units disabled on removal) and in the queue order.
func TestCrowdsecBareUninstallAndOrder(t *testing.T) {
	u, ok := serviceUninstalls["crowdsec-bare"]
	if !ok || len(u.units) == 0 {
		t.Fatal("crowdsec-bare must declare systemd units to disable")
	}
	svc := ProvisionServices{"crowdsec-bare": {Profile: "lite"}, "nats-cluster": {Profile: "lite"}}
	ordered := orderedServiceNames(svc)
	if len(ordered) != 2 || ordered[0] != "nats-cluster" || ordered[1] != "crowdsec-bare" {
		t.Errorf("order wrong: %v", ordered)
	}
}

// TestCrowdsecSizingOverrides locks the per-component sizing overrides: they
// exist so AppSec can fit into fleets whose scheduling requests are already
// saturated, on top of the profile values.
func TestCrowdsecSizingOverrides(t *testing.T) {
	prof := map[string]any{
		"lapi_cpu": "250m", "lapi_cpu_limit": "1", "lapi_mem": "256Mi", "lapi_mem_limit": "512Mi",
		"agent_cpu": "250m", "agent_cpu_limit": "500m", "agent_mem": "128Mi", "agent_mem_limit": "256Mi",
	}
	t.Setenv("CS_K8S_LAPI_CPU", "150m")
	t.Setenv("CS_K8S_AGENT_MEM", "96Mi")
	t.Setenv("CS_K8S_APPSEC_CPU", "50m")
	data, err := crowdsecClusterRenderData(prof)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if data["LapiCPU"] != "150m" {
		t.Errorf("LapiCPU override not honored: %v", data["LapiCPU"])
	}
	if data["AgentMem"] != "96Mi" {
		t.Errorf("AgentMem override not honored: %v", data["AgentMem"])
	}
	if data["AppSecCPU"] != "50m" {
		t.Errorf("AppSecCPU override not honored: %v", data["AppSecCPU"])
	}
	if data["AppSecMem"] != "128Mi" {
		t.Errorf("AppSecMem default broken: %v", data["AppSecMem"])
	}
	if data["LapiMem"] != "256Mi" || data["AgentCPU"] != "250m" {
		t.Errorf("profile fallback broken: lapi_mem=%v agent_cpu=%v", data["LapiMem"], data["AgentCPU"])
	}
}
