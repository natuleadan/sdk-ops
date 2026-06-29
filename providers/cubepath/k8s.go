package cubepath

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

func val(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

type nodePoolCreate struct {
	Name      string `json:"name,omitempty"`
	Plan      string `json:"plan"`
	NodeCount int    `json:"count,omitempty"`
}

type k8sCreateRequest struct {
	Name         string           `json:"name"`
	Location     string           `json:"location_name"`
	Version      string           `json:"version,omitempty"`
	NodePools    []nodePoolCreate `json:"node_pools"`
	ProjectID    int              `json:"project_id"`
	HAControlPlane bool           `json:"ha_control_plane,omitempty"`
	AllocateIPv4 bool             `json:"allocate_ipv4,omitempty"`
	AllocateIPv6 bool             `json:"allocate_ipv6,omitempty"`
}

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	poolName := "default"
	if cfg.Label != "" {
		poolName = cfg.Label
	}

	body := k8sCreateRequest{
		Name:      cfg.Name,
		Location:  cfg.Location,
		Version:   cfg.Version,
		ProjectID: c.projectID,
		NodePools: []nodePoolCreate{{
			Name:      poolName,
			Plan:      cfg.NodePlan,
			NodeCount: cfg.NodeCount,
		}},
		AllocateIPv4: true,
		AllocateIPv6: true,
	}
	if cfg.NodeCount < 1 {
		body.NodePools[0].NodeCount = 1
	}
	if cfg.NodePlan == "" {
		body.NodePools[0].Plan = "gp.nano"
	}

	resp, err := c.do("POST", "/kubernetes/", body)
	if err != nil {
		return nil, fmt.Errorf("cubepath create k8s: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	cluster := &providers.K8sCluster{
		ID:        val(r, "uuid"),
		Name:      val(r, "name"),
		Status:    val(r, "status"),
		Version:   val(r, "version"),
		NodeCount: cfg.NodeCount,
	}
	return cluster, nil
}

func (c *Client) DeleteK8s(ctx context.Context, id string) error {
	_, err := c.do("DELETE", "/kubernetes/"+id, nil)
	return err
}

func (c *Client) ListK8s(ctx context.Context) ([]providers.K8sCluster, error) {
	resp, err := c.do("GET", "/kubernetes/", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list k8s: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	var result []providers.K8sCluster
	for _, r := range list {
		result = append(result, providers.K8sCluster{
			ID:     val(r, "uuid"),
			Name:   val(r, "name"),
			Status: val(r, "status"),
		})
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	list, err := c.ListK8s(ctx)
	if err != nil {
		return nil, err
	}
	for _, cl := range list {
		if cl.ID == id {
			return &cl, nil
		}
	}
	return nil, fmt.Errorf("k8s %s not found", id)
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	resp, err := c.do("GET", "/kubernetes/"+id+"/kubeconfig", nil)
	if err != nil {
		return "", fmt.Errorf("cubepath kubeconfig: %w", err)
	}
	return string(resp), nil
}
