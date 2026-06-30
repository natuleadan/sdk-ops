package digitalocean

import (
	"context"
	"fmt"

	"github.com/digitalocean/godo"

	"github.com/natuleadan/sdk-ops/providers"
)

func clusterStatus(s *godo.KubernetesClusterStatus) string {
	if s == nil {
		return "unknown"
	}
	return string(s.State)
}

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	cluster, _, err := c.client.Kubernetes.Create(ctx, &godo.KubernetesClusterCreateRequest{
		Name:       cfg.Name,
		RegionSlug: cfg.Location,
		VersionSlug: cfg.Version,
		NodePools: []*godo.KubernetesNodePoolCreateRequest{{
			Name:  "default",
			Size:  cfg.NodePlan,
			Count: cfg.NodeCount,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("do create k8s: %w", err)
	}
	return &providers.K8sCluster{
		ID:        cluster.ID,
		Name:      cluster.Name,
		Status:    clusterStatus(cluster.Status),
		Location:  cfg.Location,
		Version:   cluster.VersionSlug,
		NodeCount: cfg.NodeCount,
	}, nil
}

func (c *Client) DeleteK8s(ctx context.Context, id string) error {
	_, err := c.client.Kubernetes.Delete(ctx, id)
	return err
}

func (c *Client) ListK8s(ctx context.Context) ([]providers.K8sCluster, error) {
	clusters, _, err := c.client.Kubernetes.List(ctx, &godo.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("do list k8s: %w", err)
	}
	var result []providers.K8sCluster
	for _, cl := range clusters {
		nc := 0
		for _, np := range cl.NodePools {
			nc += np.Count
		}
		result = append(result, providers.K8sCluster{
			ID: cl.ID, Name: cl.Name, Status: clusterStatus(cl.Status), Version: cl.VersionSlug, NodeCount: nc,
		})
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	cl, _, err := c.client.Kubernetes.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("do get k8s: %w", err)
	}
	return &providers.K8sCluster{ID: cl.ID, Name: cl.Name, Status: clusterStatus(cl.Status), Version: cl.VersionSlug}, nil
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	kc, _, err := c.client.Kubernetes.GetKubeConfig(ctx, id, &godo.KubernetesClusterKubeconfigGetRequest{})
	if err != nil {
		return "", fmt.Errorf("do kubeconfig: %w", err)
	}
	return string(kc.KubeconfigYAML), nil
}

func (c *Client) UpdateK8s(ctx context.Context, id, version string) (*providers.K8sCluster, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ToggleK8sProtection(ctx context.Context, id string) (*providers.K8sCluster, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ListK8sAddons(ctx context.Context, id string) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ListAvailableAddons(ctx context.Context) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) InstallK8sAddon(ctx context.Context, id, slug string) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) UninstallK8sAddon(ctx context.Context, id, addonID string) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ListK8sNodePools(ctx context.Context, id string) ([]providers.K8sNodePool, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) CreateK8sNodePool(ctx context.Context, id string, cfg providers.K8sNodePoolConfig) (*providers.K8sNodePool, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ScaleK8sNodePool(ctx context.Context, id, poolID string, nodes int) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) DeleteK8sNodePool(ctx context.Context, id, poolID string) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ListK8sLBs(ctx context.Context, id string) ([]providers.LoadBalancer, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}
