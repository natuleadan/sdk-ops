package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateK8s(ctx context.Context, cfg providers.K8sCreateConfig) (*providers.K8sCluster, error) {
	roleArn := cfg.Label
	if roleArn == "" {
		return nil, fmt.Errorf("aws eks: set cfg.Label to the EKS role ARN")
	}
	cluster, err := c.eksClient.CreateCluster(ctx, &eks.CreateClusterInput{
		Name:    &cfg.Name,
		Version: &cfg.Version,
		RoleArn: &roleArn,
		ResourcesVpcConfig: &types.VpcConfigRequest{
			EndpointPublicAccess: new(true),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws create eks: %w", err)
	}
	return &providers.K8sCluster{
		ID:       aws.ToString(cluster.Cluster.Name),
		Name:     aws.ToString(cluster.Cluster.Name),
		Status:   string(cluster.Cluster.Status),
		Location: cfg.Location,
		Version:  aws.ToString(cluster.Cluster.Version),
	}, nil
}

func (c *Client) DeleteK8s(ctx context.Context, id string) error {
	_, err := c.eksClient.DeleteCluster(ctx, &eks.DeleteClusterInput{Name: &id})
	return err
}

func (c *Client) ListK8s(ctx context.Context) ([]providers.K8sCluster, error) {
	clusters, err := c.eksClient.ListClusters(ctx, &eks.ListClustersInput{})
	if err != nil {
		return nil, fmt.Errorf("aws list eks: %w", err)
	}
	var result []providers.K8sCluster
	for _, name := range clusters.Clusters {
		desc, err := c.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &name})
		if err != nil {
			continue
		}
		result = append(result, providers.K8sCluster{
			ID:      aws.ToString(desc.Cluster.Name),
			Name:    aws.ToString(desc.Cluster.Name),
			Status:  string(desc.Cluster.Status),
			Version: aws.ToString(desc.Cluster.Version),
		})
	}
	return result, nil
}

func (c *Client) GetK8s(ctx context.Context, id string) (*providers.K8sCluster, error) {
	desc, err := c.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &id})
	if err != nil {
		return nil, fmt.Errorf("aws get eks: %w", err)
	}
	return &providers.K8sCluster{
		ID: aws.ToString(desc.Cluster.Name), Name: aws.ToString(desc.Cluster.Name),
		Status: string(desc.Cluster.Status), Version: aws.ToString(desc.Cluster.Version),
	}, nil
}

func (c *Client) GetKubeconfig(ctx context.Context, id string) (string, error) {
	desc, err := c.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &id})
	if err != nil {
		return "", fmt.Errorf("aws describe eks: %w", err)
	}

	caData := aws.ToString(desc.Cluster.CertificateAuthority.Data)
	endpoint := aws.ToString(desc.Cluster.Endpoint)

	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    certificate-authority-data: %s
  name: %s
contexts:
- context:
    cluster: %s
    user: aws
  name: %s
current-context: %s
users:
- name: aws
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args:
      - eks
      - get-token
      - --cluster-name
      - %s
`, endpoint, caData, id, id, id, id, id), nil
}

func (c *Client) UpdateK8s(ctx context.Context, id, version string) (*providers.K8sCluster, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) ToggleK8sProtection(ctx context.Context, id string) (*providers.K8sCluster, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) ListK8sAddons(ctx context.Context, id string) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) ListAvailableAddons(ctx context.Context) ([]providers.K8sAddon, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) InstallK8sAddon(ctx context.Context, id, slug string) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) UninstallK8sAddon(ctx context.Context, id, addonID string) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) ListK8sNodePools(ctx context.Context, id string) ([]providers.K8sNodePool, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) CreateK8sNodePool(ctx context.Context, id string, cfg providers.K8sNodePoolConfig) (*providers.K8sNodePool, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) ScaleK8sNodePool(ctx context.Context, id, poolID string, nodes int) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) DeleteK8sNodePool(ctx context.Context, id, poolID string) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) ListK8sLBs(ctx context.Context, id string) ([]providers.LoadBalancer, error) {
	return nil, fmt.Errorf("aws: method not available")
}
