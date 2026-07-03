package vultr

import (
	"context"
	"fmt"
	"log"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateSSHKey(ctx context.Context, cfg providers.SSHKeyCreateConfig) (*providers.SSHKey, error) {
	key, resp, err := c.client.SSHKey.Create(ctx, &govultr.SSHKeyReq{
		Name:   cfg.Name,
		SSHKey: cfg.PublicKey,
	})
	if resp != nil {
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr create ssh key: %w", err)
	}
	return &providers.SSHKey{
		ID:   key.ID,
		Name: key.Name,
	}, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]providers.SSHKey, error) {
	keys, _, resp, err := c.client.SSHKey.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list ssh keys: %w", err)
	}
	var result []providers.SSHKey
	for _, k := range keys {
		result = append(result, providers.SSHKey{
			ID: k.ID, Name: k.Name,
		})
	}
	return result, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	return c.client.SSHKey.Delete(ctx, id)
}
