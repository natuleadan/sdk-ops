package hetzner

import (
	"context"
	"fmt"
	"log"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	opts := hcloud.ServerCreateOpts{
		Name:       cfg.Hostname,
		ServerType: &hcloud.ServerType{Name: cfg.Plan},
		Location:   &hcloud.Location{Name: cfg.Location},
		Image:      &hcloud.Image{Name: cfg.Template},
		Labels:     map[string]string{"label": cfg.Label},
		UserData:   cfg.UserData,
	}
	if len(cfg.SSHKeyIDs) > 0 {
		var sshKeys []*hcloud.SSHKey
		for _, id := range cfg.SSHKeyIDs {
			sshKeys = append(sshKeys, &hcloud.SSHKey{ID: int64(id)})
		}
		opts.SSHKeys = sshKeys
	}
	server, _, err := c.client.Server.Create(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("hetzner create server: %w", err)
	}
	vps := &providers.VPS{
		ID:       fmt.Sprintf("%d", server.Server.ID),
		Name:     server.Server.Name,
		Label:    server.Server.Labels["label"],
		Status:   string(server.Server.Status),
		Plan:     cfg.Plan,
		Location: cfg.Location,
	}
	if ip := server.Server.PublicNet.IPv4.IP; ip != nil {
		vps.IP = ip.String()
	}
	return vps, nil
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	var idInt int64
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil { log.Printf("hetzner: parse id error: %v", err) }
	_, _, err := c.client.Server.DeleteWithResult(ctx, &hcloud.Server{ID: idInt})
	if err != nil {
		return fmt.Errorf("hetzner delete server: %w", err)
	}
	return nil
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	servers, _, err := c.client.Server.List(ctx, hcloud.ServerListOpts{})
	if err != nil {
		return nil, fmt.Errorf("hetzner list servers: %w", err)
	}
	var result []providers.VPS
	for _, s := range servers {
		v := providers.VPS{
			ID:   fmt.Sprintf("%d", s.ID),
			Name: s.Name,
		}
		if ip := s.PublicNet.IPv4.IP; ip != nil {
			v.IP = ip.String()
		}
		result = append(result, v)
	}
	return result, nil
}

func (c *Client) GetVPS(ctx context.Context, id string) (*providers.VPS, error) {
	var idInt int64
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil { log.Printf("hetzner: parse id error: %v", err) }
	s, _, err := c.client.Server.GetByID(ctx, idInt)
	if err != nil {
		return nil, fmt.Errorf("hetzner get server: %w", err)
	}
	if s == nil {
		return nil, fmt.Errorf("hetzner server %s not found", id)
	}
	v := &providers.VPS{
		ID:   fmt.Sprintf("%d", s.ID),
		Name: s.Name,
	}
	if ip := s.PublicNet.IPv4.IP; ip != nil {
		v.IP = ip.String()
	}
	return v, nil
}
