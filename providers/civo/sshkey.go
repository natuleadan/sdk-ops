package civo

import (
	"context"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateSSHKey(ctx context.Context, cfg providers.SSHKeyCreateConfig) (*providers.SSHKey, error) {
	resp, err := c.client.NewSSHKey(cfg.Name, cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("civo: create ssh key: %w", err)
	}
	return &providers.SSHKey{ID: resp.ID, Name: cfg.Name}, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]providers.SSHKey, error) {
	keys, err := c.client.ListSSHKeys()
	if err != nil {
		return nil, fmt.Errorf("civo: list ssh keys: %w", err)
	}
	var result []providers.SSHKey
	for _, k := range keys {
		result = append(result, providers.SSHKey{
			ID:        k.ID,
			Name:      k.Name,
			PublicKey: k.PublicKey,
		})
	}
	return result, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	_, err := c.client.DeleteSSHKey(id)
	if err != nil {
		return fmt.Errorf("civo: delete ssh key: %w", err)
	}
	return nil
}
