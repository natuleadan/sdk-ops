package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"
)

type ObjectStorage struct {
	ID          string `json:"id"`
	ClusterID   int    `json:"cluster_id"`
	DateCreated string `json:"date_created"`
	Region      string `json:"region"`
	Location    string `json:"location"`
	Label       string `json:"label"`
	S3Hostname  string `json:"s3_hostname"`
	S3AccessKey string `json:"s3_access_key"`
	S3SecretKey string `json:"s3_secret_key"`
	Status      string `json:"status"`
}

type S3Keys struct {
	S3AccessKey string `json:"s3_access_key"`
	S3SecretKey string `json:"s3_secret_key"`
}

type ObjectStorageCluster struct {
	ID          int    `json:"id"`
	Region      string `json:"region"`
	Hostname    string `json:"hostname"`
	Deploy      string `json:"deploy"`
}

func (c *Client) ListObjectStorages(ctx context.Context) ([]ObjectStorage, error) {
	objs, _, resp, err := c.client.ObjectStorage.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list object storage: %w", err)
	}
	var result []ObjectStorage
	for _, o := range objs {
		result = append(result, ObjectStorage{
			ID: o.ID, ClusterID: o.ObjectStoreClusterID, DateCreated: o.DateCreated,
			Region: o.Region, Location: o.Location, Label: o.Label,
			S3Hostname: o.S3Hostname, Status: o.Status,
			S3AccessKey: o.S3AccessKey, S3SecretKey: o.S3SecretKey,
		})
	}
	return result, nil
}

func (c *Client) CreateObjectStorage(ctx context.Context, clusterID int) (*ObjectStorage, error) {
	obj, resp, err := c.client.ObjectStorage.Create(ctx, &govultr.ObjectStorageReq{ClusterID: clusterID})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr create object storage: %w", err)
	}
	return &ObjectStorage{
		ID: obj.ID, ClusterID: obj.ObjectStoreClusterID, DateCreated: obj.DateCreated,
		Region: obj.Region, Location: obj.Location, Label: obj.Label,
		S3Hostname: obj.S3Hostname, Status: obj.Status,
		S3AccessKey: obj.S3AccessKey, S3SecretKey: obj.S3SecretKey,
	}, nil
}

func (c *Client) GetObjectStorage(ctx context.Context, id string) (*ObjectStorage, error) {
	obj, resp, err := c.client.ObjectStorage.Get(ctx, id)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr get object storage: %w", err)
	}
	return &ObjectStorage{
		ID: obj.ID, ClusterID: obj.ObjectStoreClusterID, DateCreated: obj.DateCreated,
		Region: obj.Region, Location: obj.Location, Label: obj.Label,
		S3Hostname: obj.S3Hostname, Status: obj.Status,
		S3AccessKey: obj.S3AccessKey, S3SecretKey: obj.S3SecretKey,
	}, nil
}

func (c *Client) DeleteObjectStorage(ctx context.Context, id string) error {
	return c.client.ObjectStorage.Delete(ctx, id)
}

func (c *Client) RegenerateS3Keys(ctx context.Context, id string) (*S3Keys, error) {
	keys, resp, err := c.client.ObjectStorage.RegenerateKeys(ctx, id)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr regenerate s3 keys: %w", err)
	}
	return &S3Keys{S3AccessKey: keys.S3AccessKey, S3SecretKey: keys.S3SecretKey}, nil
}

func (c *Client) ListObjectStorageClusters(ctx context.Context) ([]ObjectStorageCluster, error) {
	clusters, _, resp, err := c.client.ObjectStorage.ListCluster(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list object storage clusters: %w", err)
	}
	var result []ObjectStorageCluster
	for _, cl := range clusters {
		result = append(result, ObjectStorageCluster{
			ID: cl.ID, Region: cl.Region, Hostname: cl.Hostname, Deploy: cl.Deploy,
		})
	}
	return result, nil
}
