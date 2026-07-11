package vultr

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func osIDFromTemplate(template string) int {
	switch template {
	case "ubuntu-22", "ubuntu22":
		return 1743
	case "ubuntu-24", "ubuntu24":
		return 2284
	case "ubuntu-26", "ubuntu26", "ubuntu":
		return 2760
	case "debian-12", "debian12":
		return 2577
	default:
		return 2760 // ubuntu 26
	}
}

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	sshKeys := make([]string, len(cfg.SSHKeyIDs))
	for i, id := range cfg.SSHKeyIDs {
		sshKeys[i] = fmt.Sprintf("%d", id)
	}

	instance, resp, err := c.client.Instance.Create(ctx, &govultr.InstanceCreateReq{
		Label:    cfg.Label,
		Plan:     cfg.Plan,
		Region:   cfg.Location,
		Hostname: cfg.Hostname,
		OsID:     osIDFromTemplate(cfg.Template),
		SSHKeys:  sshKeys,
		UserData: cfg.UserData,
	})
	if resp != nil {
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
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

	fmt.Printf("  Waiting for VPS %s to get IP...\n", instance.ID)
	for range 30 {
		time.Sleep(5 * time.Second)
		current, err := c.GetVPS(ctx, instance.ID)
		if err == nil && current.IP != "" && current.IP != "0.0.0.0" {
			v.IP = current.IP
			v.Status = current.Status
			fmt.Printf("  ✅ VPS %s active @ %s\n", v.ID, v.IP)
			break
		}
	}

	return v, nil
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	err := c.client.Instance.Delete(ctx, id)
	return err
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	instances, _, resp, err := c.client.Instance.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list instances: %w", err)
	}
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
		defer func() { if err := resp.Body.Close(); err != nil { log.Printf("vultr: body close error: %v", err) } }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr get instance: %w", err)
	}
	return &providers.VPS{ID: inst.ID, Name: inst.Label, Status: inst.Status, IP: inst.MainIP}, nil
}
