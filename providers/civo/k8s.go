package civo

import (
	"context"
	"fmt"
	"strings"

	"github.com/civo/civogo"
	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	pool := civogo.KubernetesClusterPoolConfig{
		Size:  cfg.NodePlan,
		Count: cfg.NodeCount,
	}
	req := &civogo.KubernetesClusterConfig{
		Name:   cfg.Name,
		Region: regionAlias(cfg.Location),
		Pools:  []civogo.KubernetesClusterPoolConfig{pool},
	}
	if cfg.Version != "" {
		req.KubernetesVersion = cfg.Version
	}

	cluster, err := c.client.NewKubernetesClusters(req)
	if err != nil {
		return nil, fmt.Errorf("civo: create k8s: %w", err)
	}

	fmt.Printf("  Cluster %s provisioning...\n", cluster.ID)
	return clusterToProvider(cluster), nil
}

func clusterToProvider(cl *civogo.KubernetesCluster) *providers.K8sCluster {
	return &providers.K8sCluster{
		ID:      cl.ID,
		Name:    cl.Name,
		Status:  strings.ToLower(cl.Status),
		Version: cl.KubernetesVersion,
	}
}

func (c *Client) ListK8s(ctx context.Context) ([]providers.K8sCluster, error) {
	clusters, err := c.client.ListKubernetesClusters()
	if err != nil {
		return nil, fmt.Errorf("civo: list k8s: %w", err)
	}
	var result []providers.K8sCluster
	for _, cl := range clusters.Items {
		result = append(result, *clusterToProvider(&cl))
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	cl, err := c.client.GetKubernetesCluster(id)
	if err != nil {
		return nil, fmt.Errorf("civo: get k8s: %w", err)
	}
	return clusterToProvider(cl), nil
}

func (c *Client) DeleteK8s(ctx context.Context, id string) error {
	knownRegions := []string{"NYC1", "LON1", "FRA1", "MUM1", "PHX1"}
	var lastErr error
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = cc.DeleteKubernetesCluster(id)
		if err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("civo: delete k8s: %w", lastErr)
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	knownRegions := []string{"NYC1", "LON1", "FRA1", "MUM1", "PHX1"}
	var lastErr error
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil {
			lastErr = err
			continue
		}
		cl, err := cc.GetKubernetesCluster(id)
		if err != nil {
			lastErr = err
			continue
		}
		if cl.KubeConfig == "" {
			return "", fmt.Errorf("civo: kubeconfig not ready yet")
		}
		return cl.KubeConfig, nil
	}
	return "", fmt.Errorf("civo: get kubeconfig: %w", lastErr)
}

func (c *Client) UpdateK8s(ctx context.Context, id, version string) (*providers.K8sCluster, error) {
	req := &civogo.KubernetesClusterConfig{KubernetesVersion: version}
	cl, err := c.client.UpdateKubernetesCluster(id, req)
	if err != nil {
		return nil, fmt.Errorf("civo: update k8s: %w", err)
	}
	return clusterToProvider(cl), nil
}

func (c *Client) ToggleK8sProtection(ctx context.Context, id string) (*providers.K8sCluster, error) {
	return nil, fmt.Errorf("civo: method not available")
}

func (c *Client) ListK8sAddons(ctx context.Context, id string) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("civo: method not available")
}

func (c *Client) ListAvailableAddons(ctx context.Context) ([]providers.K8sAddon, error) {
	addons, err := c.client.ListKubernetesMarketplaceApplications()
	if err != nil {
		return nil, fmt.Errorf("civo: list addons: %w", err)
	}
	var result []providers.K8sAddon
	for _, a := range addons {
		result = append(result, providers.K8sAddon{
			ID:   a.Name,
			Name: a.Title,
			Slug: a.Name,
		})
	}
	return result, nil
}

func (c *Client) InstallK8sAddon(ctx context.Context, id, slug string) error {
	return fmt.Errorf("civo: method not available")
}

func (c *Client) UninstallK8sAddon(ctx context.Context, id, addonID string) error {
	return fmt.Errorf("civo: method not available")
}

func (c *Client) ListK8sNodePools(ctx context.Context, id string) ([]providers.K8sNodePool, error) {
	pools, err := c.client.ListKubernetesClusterPools(id)
	if err != nil {
		return nil, fmt.Errorf("civo: list node pools: %w", err)
	}
	var result []providers.K8sNodePool
	for _, p := range pools {
		result = append(result, providers.K8sNodePool{
			ID:     p.ID,
			Name:   p.ID,
			Plan:   p.Size,
			Nodes:  p.Count,
			Status: "active",
		})
	}
	return result, nil
}

func (c *Client) CreateK8sNodePool(ctx context.Context, id string, cfg providers.K8sNodePoolConfig) (*providers.K8sNodePool, error) {
	resp, err := c.client.CreateKubernetesClusterPool(id, &civogo.KubernetesClusterPoolConfig{
		Size:  cfg.Plan,
		Count: cfg.NodeCount,
	})
	if err != nil {
		return nil, fmt.Errorf("civo: create node pool: %w", err)
	}
	return &providers.K8sNodePool{
		ID:    resp.ID,
		Name:  resp.ID,
		Plan:  cfg.Plan,
		Nodes: cfg.NodeCount,
	}, nil
}

func (c *Client) ScaleK8sNodePool(ctx context.Context, id, poolID string, nodes int) error {
	n := nodes
	_, err := c.client.UpdateKubernetesClusterPool(id, poolID, &civogo.KubernetesClusterPoolUpdateConfig{
		Count: &n,
	})
	if err != nil {
		return fmt.Errorf("civo: scale node pool: %w", err)
	}
	return nil
}

func (c *Client) DeleteK8sNodePool(ctx context.Context, id, poolID string) error {
	_, err := c.client.DeleteKubernetesClusterPool(id, poolID)
	if err != nil {
		return fmt.Errorf("civo: delete node pool: %w", err)
	}
	return nil
}

func (c *Client) ListK8sLBs(ctx context.Context, id string) ([]providers.LoadBalancer, error) {
	return nil, fmt.Errorf("civo: method not available")
}
