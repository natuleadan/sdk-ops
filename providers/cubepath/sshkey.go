package cubepath

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateSSHKey(ctx context.Context, cfg providers.SSHKeyCreateConfig) (*providers.SSHKey, error) {
	body := map[string]any{
		"name":    cfg.Name,
		"ssh_key": cfg.PublicKey,
	}
	resp, err := c.do("POST", "/sshkey/create", body)
	if err != nil {
		return nil, fmt.Errorf("cubepath create ssh key: %w", err)
	}
	var result struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.SSHKey{ID: fmt.Sprintf("%d", result.ID), Name: result.Name, Fingerprint: result.Fingerprint}, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]providers.SSHKey, error) {
	resp, err := c.do("GET", "/sshkey/user/sshkeys", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list ssh keys: %w", err)
	}
	var wrapped struct {
		Keys []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Fingerprint string `json:"fingerprint"`
			PublicKey   string `json:"ssh_key"`
		} `json:"sshkeys"`
	}
	if err := json.Unmarshal(resp, &wrapped); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.SSHKey
	for _, k := range wrapped.Keys {
		result = append(result, providers.SSHKey{
			ID: fmt.Sprintf("%d", k.ID), Name: k.Name,
			Fingerprint: k.Fingerprint, PublicKey: k.PublicKey,
		})
	}
	return result, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	_, err := c.do("DELETE", "/sshkey/"+id, nil)
	return err
}
