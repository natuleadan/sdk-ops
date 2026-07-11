package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"
)

type BlockStorage struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	SizeGB     int    `json:"size_gb"`
	Region     string `json:"region"`
	Status     string `json:"status"`
	AttachedTo string `json:"attached_to"`
}

func (c *Client) ListBlockStorages(ctx context.Context) ([]BlockStorage, error) {
	blocks, _, resp, err := c.client.BlockStorage.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr block list: %w", err)
	}
	var result []BlockStorage
	for _, b := range blocks {
		result = append(result, BlockStorage{
			ID: b.ID, Label: b.Label, SizeGB: b.SizeGB, Region: b.Region, Status: b.Status,
			AttachedTo: b.AttachedToInstance,
		})
	}
	return result, nil
}

func (c *Client) CreateBlockStorage(ctx context.Context, label, region string, sizeGB int) (*BlockStorage, error) {
	block, resp, err := c.client.BlockStorage.Create(ctx, &govultr.BlockStorageCreate{
		Label: label, Region: region, SizeGB: sizeGB,
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr block create: %w", err)
	}
	return &BlockStorage{ID: block.ID, Label: block.Label, SizeGB: block.SizeGB, Region: block.Region, Status: block.Status}, nil
}

func (c *Client) DeleteBlockStorage(ctx context.Context, id string) error {
	return c.client.BlockStorage.Delete(ctx, id)
}

func (c *Client) AttachBlockStorage(ctx context.Context, id, instanceID string) error {
	return c.client.BlockStorage.Attach(ctx, id, &govultr.BlockStorageAttach{InstanceID: instanceID})
}

func (c *Client) DetachBlockStorage(ctx context.Context, id string) error {
	return c.client.BlockStorage.Detach(ctx, id, &govultr.BlockStorageDetach{})
}
