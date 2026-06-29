package hetzner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

// Client embeds raw client for K8s/LB/DNS (not in hcloud-go v2)
func (c *Client) raw() *rawClient {
	return newRawClient(c.token)
}

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	body := map[string]any{
		"name":     cfg.Name,
		"location": cfg.Location,
		"version":  cfg.Version,
		"networks": []string{},
		"nodepools": []map[string]any{{
			"name":   "default",
			"server_type": cfg.NodePlan,
			"count": cfg.NodeCount,
		}},
	}
	resp, err := c.raw().do("POST", "/kubernetes/clusters", body)
	if err != nil {
		return nil, fmt.Errorf("hetzner create k8s: %w", err)
	}
	var r struct {
		Cluster map[string]any `json:"cluster"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.K8sCluster{
		ID:     val(r.Cluster, "id"),
		Name:   val(r.Cluster, "name"),
		Status: val(r.Cluster, "status"),
	}, nil
}

func (c *Client) DeleteK8s(ctx context.Context, id string) error {
	_, err := c.raw().do("DELETE", "/kubernetes/clusters/"+id, nil)
	return err
}

func (c *Client) ListK8s(ctx context.Context) ([]providers.K8sCluster, error) {
	resp, err := c.raw().do("GET", "/kubernetes/clusters", nil)
	if err != nil {
		return nil, fmt.Errorf("hetzner list k8s: %w", err)
	}
	var r struct {
		Clusters []map[string]any `json:"clusters"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.K8sCluster
	for _, cl := range r.Clusters {
		result = append(result, providers.K8sCluster{ID: val(cl, "id"), Name: val(cl, "name"), Status: val(cl, "status")})
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	resp, err := c.raw().do("GET", "/kubernetes/clusters/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("hetzner get k8s: %w", err)
	}
	var r struct {
		Cluster map[string]any `json:"cluster"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.K8sCluster{ID: val(r.Cluster, "id"), Name: val(r.Cluster, "name"), Status: val(r.Cluster, "status")}, nil
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	resp, err := c.raw().do("GET", "/kubernetes/clusters/"+id+"/kubeconfig", nil)
	if err != nil {
		return "", fmt.Errorf("hetzner kubeconfig: %w", err)
	}
	return string(resp), nil
}
