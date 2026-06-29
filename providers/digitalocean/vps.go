package digitalocean

import (
	"context"
	"fmt"
	"strconv"

	"github.com/digitalocean/godo"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	droplet, _, err := c.client.Droplets.Create(ctx, &godo.DropletCreateRequest{
		Name:     cfg.Hostname,
		Region:   cfg.Location,
		Size:     cfg.Plan,
		Image:    godo.DropletCreateImage{Slug: cfg.Template},
		Tags:     []string{cfg.Label},
		UserData: cfg.UserData,
	})
	if err != nil {
		return nil, fmt.Errorf("do create droplet: %w", err)
	}
	v := &providers.VPS{
		ID:       strconv.Itoa(droplet.ID),
		Name:     droplet.Name,
		Label:    cfg.Label,
		Status:   droplet.Status,
		Plan:     cfg.Plan,
		Location: cfg.Location,
	}
	if len(droplet.Networks.V4) > 0 {
		v.IP = droplet.Networks.V4[0].IPAddress
	}
	return v, nil
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	idInt, _ := strconv.Atoi(id)
	_, err := c.client.Droplets.Delete(ctx, idInt)
	return err
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	droplets, _, err := c.client.Droplets.List(ctx, &godo.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("do list droplets: %w", err)
	}
	var result []providers.VPS
	for _, d := range droplets {
		v := providers.VPS{ID: strconv.Itoa(d.ID), Name: d.Name, Status: d.Status}
		if len(d.Networks.V4) > 0 {
			v.IP = d.Networks.V4[0].IPAddress
		}
		result = append(result, v)
	}
	return result, nil
}

func (c *Client) GetVPS(ctx context.Context, id string) (*providers.VPS, error) {
	idInt, _ := strconv.Atoi(id)
	d, _, err := c.client.Droplets.Get(ctx, idInt)
	if err != nil {
		return nil, fmt.Errorf("do get droplet: %w", err)
	}
	v := &providers.VPS{ID: strconv.Itoa(d.ID), Name: d.Name, Status: d.Status}
	if len(d.Networks.V4) > 0 {
		v.IP = d.Networks.V4[0].IPAddress
	}
	return v, nil
}
