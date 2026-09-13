package main

import (
	"strings"
	"testing"
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
