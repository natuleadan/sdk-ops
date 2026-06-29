package hetzner

import (
	"context"
	"fmt"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateSSHKey(ctx context.Context, cfg providers.SSHKeyCreateConfig) (*providers.SSHKey, error) {
	key, _, err := c.client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      cfg.Name,
		PublicKey: cfg.PublicKey,
	})
	if err != nil {
		return nil, fmt.Errorf("hetzner create ssh key: %w", err)
	}
	return &providers.SSHKey{
		ID:          fmt.Sprintf("%d", key.ID),
		Name:        key.Name,
		Fingerprint: key.Fingerprint,
		PublicKey:   key.PublicKey,
	}, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]providers.SSHKey, error) {
	keys, err := c.client.SSHKey.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("hetzner list ssh keys: %w", err)
	}
	var result []providers.SSHKey
	for _, k := range keys {
		result = append(result, providers.SSHKey{
			ID: fmt.Sprintf("%d", k.ID), Name: k.Name,
			Fingerprint: k.Fingerprint, PublicKey: k.PublicKey,
		})
	}
	return result, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	var idInt int64
	fmt.Sscanf(id, "%d", &idInt)
	_, err := c.client.SSHKey.Delete(ctx, &hcloud.SSHKey{ID: idInt})
	return err
}
