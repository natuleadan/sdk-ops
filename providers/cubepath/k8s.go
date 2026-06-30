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

func (c *Client) UpdateK8s(ctx context.Context, id, version string) (*providers.K8sCluster, error) {
	resp, err := c.do("PATCH", "/kubernetes/"+id, map[string]string{"version": version})
	if err != nil {
		return nil, fmt.Errorf("cubepath update k8s: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	return &providers.K8sCluster{ID: val(r, "uuid"), Status: val(r, "status"), Version: val(r, "version")}, nil
}

func (c *Client) ToggleK8sProtection(ctx context.Context, id string) (*providers.K8sCluster, error) {
	_, err := c.do("POST", "/kubernetes/"+id+"/protection", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath toggle protection: %w", err)
	}
	return c.GetK8s(ctx, id)
}

func (c *Client) ListK8sAddons(ctx context.Context, id string) ([]providers.K8sAddon, error) {
	resp, err := c.do("GET", "/kubernetes/"+id+"/addons", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list addons: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	var result []providers.K8sAddon
	for _, r := range list {
		result = append(result, providers.K8sAddon{
			ID:        val(r, "uuid"),
			Name:      val(r, "name"),
			Slug:      val(r, "slug"),
			Version:   val(r, "version"),
			Status:    val(r, "status"),
			Installed: val(r, "installed") == "true",
		})
	}
	return result, nil
}

func (c *Client) ListAvailableAddons(ctx context.Context) ([]providers.K8sAddon, error) {
	resp, err := c.do("GET", "/kubernetes/addons", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list available addons: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	var result []providers.K8sAddon
	for _, r := range list {
		result = append(result, providers.K8sAddon{
			ID:      val(r, "uuid"),
			Name:    val(r, "name"),
			Slug:    val(r, "slug"),
			Version: val(r, "version"),
		})
	}
	return result, nil
}

func (c *Client) InstallK8sAddon(ctx context.Context, id, slug string) error {
	_, err := c.do("POST", fmt.Sprintf("/kubernetes/%s/addons/%s/install", id, slug), nil)
	return err
}

func (c *Client) UninstallK8sAddon(ctx context.Context, id, addonID string) error {
	_, err := c.do("DELETE", fmt.Sprintf("/kubernetes/%s/addons/%s", id, addonID), nil)
	return err
}

func (c *Client) ListK8sNodePools(ctx context.Context, id string) ([]providers.K8sNodePool, error) {
	resp, err := c.do("GET", "/kubernetes/"+id+"/node-pools", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list node pools: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	var result []providers.K8sNodePool
	for _, r := range list {
		result = append(result, providers.K8sNodePool{
			ID:     val(r, "uuid"),
			Name:   val(r, "name"),
			Plan:   val(r, "plan"),
			Nodes:  atoi(val(r, "count")),
			Status: val(r, "status"),
		})
	}
	return result, nil
}

func (c *Client) CreateK8sNodePool(ctx context.Context, id string, cfg providers.K8sNodePoolConfig) (*providers.K8sNodePool, error) {
	resp, err := c.do("POST", "/kubernetes/"+id+"/node-pools", nodePoolCreate{
		Name:      cfg.Name,
		Plan:      cfg.Plan,
		NodeCount: cfg.NodeCount,
	})
	if err != nil {
		return nil, fmt.Errorf("cubepath create node pool: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	return &providers.K8sNodePool{
		ID:     val(r, "uuid"),
		Name:   val(r, "name"),
		Plan:   val(r, "plan"),
		Nodes:  atoi(val(r, "count")),
		Status: val(r, "status"),
	}, nil
}

func (c *Client) ScaleK8sNodePool(ctx context.Context, id, poolID string, nodes int) error {
	_, err := c.do("PATCH", fmt.Sprintf("/kubernetes/%s/node-pools/%s", id, poolID),
		map[string]int{"count": nodes})
	return err
}

func (c *Client) DeleteK8sNodePool(ctx context.Context, id, poolID string) error {
	_, err := c.do("DELETE", fmt.Sprintf("/kubernetes/%s/node-pools/%s", id, poolID), nil)
	return err
}

func (c *Client) ListK8sLBs(ctx context.Context, id string) ([]providers.LoadBalancer, error) {
	resp, err := c.do("GET", "/kubernetes/"+id+"/loadbalancers", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list k8s lbs: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w\nbody: %s", err, string(resp))
	}
	var result []providers.LoadBalancer
	for _, r := range list {
		result = append(result, providers.LoadBalancer{
			ID: val(r, "uuid"), Name: val(r, "name"), IP: val(r, "ipv4_address"),
		})
	}
	return result, nil
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
