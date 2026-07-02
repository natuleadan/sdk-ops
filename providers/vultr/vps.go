package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	instance, resp, err := c.client.Instance.Create(ctx, &govultr.InstanceCreateReq{
		Label:    cfg.Label,
		Plan:     cfg.Plan,
		Region:   cfg.Location,
		Hostname: cfg.Hostname,
		UserData: cfg.UserData,
	})
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr create instance: %w", err)
	}
	v := &providers.VPS{
		ID:       instance.ID,
		Name:     instance.Label,
		Label:    cfg.Label,
		Status:   instance.Status,
		Plan:     cfg.Plan,
		Location: cfg.Location,
		IP:       instance.MainIP,
	}
	return v, nil
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	err := c.client.Instance.Delete(ctx, id)
	return err
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	instances, meta, resp, err := c.client.Instance.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list instances: %w", err)
	}
	_ = meta
	var result []providers.VPS
	for _, inst := range instances {
		result = append(result, providers.VPS{
			ID: inst.ID, Name: inst.Label, Status: inst.Status, IP: inst.MainIP,
		})
	}
	return result, nil
}

func (c *Client) GetVPS(ctx context.Context, id string) (*providers.VPS, error) {
	inst, resp, err := c.client.Instance.Get(ctx, id)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr get instance: %w", err)
	}
	return &providers.VPS{ID: inst.ID, Name: inst.Label, Status: inst.Status, IP: inst.MainIP}, nil
}
