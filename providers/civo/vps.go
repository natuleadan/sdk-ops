package civo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/civo/civogo"
	"github.com/natuleadan/sdk-ops/providers"
)

func osTemplateID(c *civogo.Client, template string) string {
	images, err := c.ListDiskImages()
	if err != nil {
		return ""
	}
	switch template {
	case "ubuntu-24", "ubuntu24", "ubuntu":
		for _, img := range images {
			if strings.Contains(img.Name, "ubuntu-24") || strings.Contains(img.Name, "ubuntu-24-04") {
				return img.ID
			}
		}
	case "ubuntu-22", "ubuntu22":
		for _, img := range images {
			if strings.Contains(img.Name, "ubuntu-22") || strings.Contains(img.Name, "ubuntu-22-04") {
				return img.ID
			}
		}
	case "debian-12", "debian12":
		for _, img := range images {
			if strings.Contains(img.Name, "debian-12") {
				return img.ID
			}
		}
	}
	if len(images) > 0 {
		return images[0].ID
	}
	return ""
}

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	config, err := c.client.NewInstanceConfig()
	if err != nil {
		return nil, fmt.Errorf("civo: new instance config: %w", err)
	}

	config.Hostname = cfg.Hostname
	if config.Hostname == "" {
		config.Hostname = cfg.Label
	}
	config.Size = cfg.Plan
	config.Region = regionAlias(cfg.Location)
	config.TemplateID = osTemplateID(c.client, cfg.Template)
	config.SSHKeyID = ""
	if len(cfg.SSHKeyIDs) > 0 {
		if !strings.Contains(cfg.SSHKeyIDs[0], "-") {
			keys, err := c.client.ListSSHKeys()
			if err == nil && len(keys) > 0 {
				config.SSHKeyID = keys[0].ID
			}
		} else {
			config.SSHKeyID = cfg.SSHKeyIDs[0]
		}
	}
	if config.TemplateID == "" {
		return nil, fmt.Errorf("civo: no valid template found for %q", cfg.Template)
	}

	instance, err := c.client.CreateInstance(config)
	if err != nil {
		return nil, fmt.Errorf("civo: create instance: %w", err)
	}

	fmt.Printf("  Waiting for VPS %s to get IP...\n", instance.ID)
	for range 30 {
		time.Sleep(5 * time.Second)
		inst, err := c.client.GetInstance(instance.ID)
		if err == nil && inst.PublicIP != "" && inst.Status == "ACTIVE" {
			instance = inst
			break
		}
	}

	return &providers.VPS{
		ID:       instance.ID,
		Name:     instance.Hostname,
		Label:    instance.Hostname,
		IP:       instance.PublicIP,
		IPv6:     instance.IPv6,
		Status:   strings.ToLower(instance.Status),
		Plan:     instance.Size,
		Location: instance.Region,
		Template: cfg.Template,
	}, nil
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	knownRegions := []string{"NYC1", "LON1", "FRA1", "MUM1", "PHX1"}
	var result []providers.VPS
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil {
			continue
		}
		instances, err := cc.ListAllInstances()
		if err != nil {
			continue
		}
		for _, inst := range instances {
			result = append(result, providers.VPS{
				ID:       inst.ID,
				Name:     inst.Hostname,
				Label:    inst.Hostname,
				IP:       inst.PublicIP,
				IPv6:     inst.IPv6,
				Status:   strings.ToLower(inst.Status),
				Plan:     inst.Size,
				Location: inst.Region,
			})
		}
	}
	return result, nil
}

func (c *Client) GetVPS(ctx context.Context, id string) (*providers.VPS, error) {
	knownRegions := []string{"NYC1", "LON1", "FRA1", "MUM1", "PHX1"}
	var lastErr error
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil {
			lastErr = err
			continue
		}
		inst, err := cc.GetInstance(id)
		if err != nil {
			lastErr = err
			continue
		}
		return &providers.VPS{
			ID:       inst.ID,
			Name:     inst.Hostname,
			Label:    inst.Hostname,
			IP:       inst.PublicIP,
			IPv6:     inst.IPv6,
			Status:   strings.ToLower(inst.Status),
			Plan:     inst.Size,
			Location: inst.Region,
		}, nil
	}
	return nil, fmt.Errorf("civo: get instance: %w", lastErr)
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	knownRegions := []string{"NYC1", "LON1", "FRA1", "MUM1", "PHX1"}
	var lastErr error
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = cc.DeleteInstance(id)
		if err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("civo: delete instance: %w", lastErr)
}
