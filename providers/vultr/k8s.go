package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	cluster, _, err := c.client.Kubernetes.CreateCluster(ctx, &govultr.ClusterReq{
		Label:   cfg.Name,
		Region:  cfg.Location,
		Version: cfg.Version,
		NodePools: []govultr.NodePoolReq{{
			NodeQuantity: cfg.NodeCount,
			Plan:         cfg.NodePlan,
			Label:        "default",
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("vultr create k8s: %w", err)
	}
	return &providers.K8sCluster{
		ID:        cluster.ID,
		Name:      cluster.Label,
		Status:    cluster.Status,
		Location:  cfg.Location,
		Version:   cluster.Version,
		NodeCount: cfg.NodeCount,
	}, nil
}

func (c *Client) DeleteK8s(ctx context.Context, id string) error {
	err := c.client.Kubernetes.DeleteCluster(ctx, id)
	return err
}

func (c *Client) ListK8s(ctx context.Context) ([]providers.K8sCluster, error) {
	clusters, meta, _, err := c.client.Kubernetes.ListClusters(ctx, &govultr.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("vultr list k8s: %w", err)
	}
	_ = meta
	var result []providers.K8sCluster
	for _, cl := range clusters {
		result = append(result, providers.K8sCluster{
			ID: cl.ID, Name: cl.Label, Status: cl.Status, Version: cl.Version,
		})
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	cl, _, err := c.client.Kubernetes.GetCluster(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("vultr get k8s: %w", err)
	}
	return &providers.K8sCluster{ID: cl.ID, Name: cl.Label, Status: cl.Status, Version: cl.Version}, nil
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	kc, _, err := c.client.Kubernetes.GetKubeConfig(ctx, id)
	if err != nil {
		return "", fmt.Errorf("vultr kubeconfig: %w", err)
	}
	return kc.KubeConfig, nil
}
