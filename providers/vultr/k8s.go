package vultr

import (
	"context"
	"fmt"
	"log"
	"slices"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	cluster, resp, err := c.client.Kubernetes.CreateCluster(ctx, &govultr.ClusterReq{
		Label:   cfg.Name,
		Region:  cfg.Location,
		Version: cfg.Version,
		NodePools: []govultr.NodePoolReq{{
			NodeQuantity: cfg.NodeCount,
			Plan:         cfg.NodePlan,
			Label:        "default",
		}},
	})
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("vultr: body close error: %v", err)
			}
		}()
	}
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
	clusters, _, resp, err := c.client.Kubernetes.ListClusters(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("vultr: body close error: %v", err)
			}
		}()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list k8s: %w", err)
	}
	var result []providers.K8sCluster
	for _, cl := range clusters {
		result = append(result, providers.K8sCluster{
			ID: cl.ID, Name: cl.Label, Status: cl.Status, Version: cl.Version,
		})
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	cl, resp, err := c.client.Kubernetes.GetCluster(ctx, id)
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("vultr: body close error: %v", err)
			}
		}()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr get k8s: %w", err)
	}
	return &providers.K8sCluster{ID: cl.ID, Name: cl.Label, Status: cl.Status, Version: cl.Version}, nil
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	kc, resp, err := c.client.Kubernetes.GetKubeConfig(ctx, id)
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("vultr: body close error: %v", err)
			}
		}()
	}
	if err != nil {
		return "", fmt.Errorf("vultr kubeconfig: %w", err)
	}
	return kc.KubeConfig, nil
}

func (c *Client) UpdateK8s(ctx context.Context, id, version string) (*providers.K8sCluster, error) {
	upgrades, resp, err := c.client.Kubernetes.GetUpgrades(ctx, id)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr get upgrades: %w", err)
	}
	valid := slices.Contains(upgrades, version)
	if !valid {
		return nil, fmt.Errorf("vultr: version %s not in available upgrades: %v", version, upgrades)
	}
	if err := c.client.Kubernetes.Upgrade(ctx, id, &govultr.ClusterUpgradeReq{UpgradeVersion: version}); err != nil {
		return nil, fmt.Errorf("vultr upgrade k8s: %w", err)
	}
	return &providers.K8sCluster{ID: id, Version: version, Status: "upgrading"}, nil
}

func (c *Client) ToggleK8sProtection(ctx context.Context, id string) (*providers.K8sCluster, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) ListK8sAddons(ctx context.Context, id string) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) ListAvailableAddons(ctx context.Context) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) InstallK8sAddon(ctx context.Context, id, slug string) error {
	return fmt.Errorf("vultr: method not available")
}

func (c *Client) UninstallK8sAddon(ctx context.Context, id, addonID string) error {
	return fmt.Errorf("vultr: method not available")
}

func (c *Client) ListK8sNodePools(ctx context.Context, id string) ([]providers.K8sNodePool, error) {
	pools, _, resp, err := c.client.Kubernetes.ListNodePools(ctx, id, &govultr.ListOptions{})
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("vultr: body close error: %v", err)
			}
		}()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list node pools: %w", err)
	}
	var result []providers.K8sNodePool
	for _, p := range pools {
		result = append(result, providers.K8sNodePool{
			ID:     p.ID,
			Name:   p.Label,
			Plan:   p.Plan,
			Nodes:  p.NodeQuantity,
			Status: p.Status,
		})
	}
	return result, nil
}

func (c *Client) CreateK8sNodePool(ctx context.Context, id string, cfg providers.K8sNodePoolConfig) (*providers.K8sNodePool, error) {
	pool, resp, err := c.client.Kubernetes.CreateNodePool(ctx, id, &govultr.NodePoolReq{
		NodeQuantity: cfg.NodeCount,
		Plan:         cfg.Plan,
		Label:        cfg.Name,
	})
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Printf("vultr: body close error: %v", err)
			}
		}()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr create node pool: %w", err)
	}
	return &providers.K8sNodePool{
		ID:     pool.ID,
		Name:   pool.Label,
		Plan:   pool.Plan,
		Nodes:  pool.NodeQuantity,
		Status: pool.Status,
	}, nil
}

func (c *Client) ScaleK8sNodePool(ctx context.Context, id, poolID string, nodes int) error {
	_, resp, err := c.client.Kubernetes.UpdateNodePool(ctx, id, poolID, &govultr.NodePoolReqUpdate{NodeQuantity: nodes})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	return err
}

func (c *Client) DeleteK8sNodePool(ctx context.Context, id, poolID string) error {
	return c.client.Kubernetes.DeleteNodePool(ctx, id, poolID)
}

func (c *Client) ListK8sLBs(ctx context.Context, id string) ([]providers.LoadBalancer, error) {
	return c.ListLB(ctx)
}
