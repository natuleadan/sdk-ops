package digitalocean

import (
	"context"
	"fmt"
	"strconv"

	"github.com/digitalocean/godo"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateBareMetal(ctx context.Context, cfg providers.BareMetalCreateConfig) (*providers.BareMetal, error) {
	return nil, fmt.Errorf("digitalocean: no bare metal support")
}

func (c *Client) DeleteBareMetal(ctx context.Context, id string) error {
	return fmt.Errorf("digitalocean: no bare metal support")
}

func (c *Client) ListBareMetal(ctx context.Context) ([]providers.BareMetal, error) {
	return nil, fmt.Errorf("digitalocean: no bare metal support")
}

func (c *Client) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	lb, _, err := c.client.LoadBalancers.Create(ctx, &godo.LoadBalancerRequest{
		Name:      cfg.Name,
		Region:    cfg.Location,
		Algorithm: cfg.Algorithm,
		Tag:       cfg.Label,
	})
	if err != nil {
		return nil, fmt.Errorf("do create lb: %w", err)
	}
	return &providers.LoadBalancer{
		ID:   lb.ID,
		Name: lb.Name,
		IP:   lb.IP,
		Status: lb.Status,
	}, nil
}

func (c *Client) DeleteLB(ctx context.Context, id string) error {
	_, err := c.client.LoadBalancers.Delete(ctx, id)
	return err
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	lbs, _, err := c.client.LoadBalancers.List(ctx, &godo.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("do list lb: %w", err)
	}
	var result []providers.LoadBalancer
	for _, lb := range lbs {
		result = append(result, providers.LoadBalancer{
			ID: lb.ID, Name: lb.Name, IP: lb.IP, Status: lb.Status,
		})
	}
	return result, nil
}

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	domains, _, err := c.client.Domains.List(ctx, &godo.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("do list domains: %w", err)
	}
	var result []providers.DNSZone
	for _, d := range domains {
		result = append(result, providers.DNSZone{ID: d.Name, Name: d.Name})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	_, _, err := c.client.Domains.CreateRecord(ctx, zoneID, &godo.DomainRecordEditRequest{
		Type:     r.Type,
		Name:     r.Name,
		Data:     r.Value,
		Priority: r.Priority,
		TTL:      r.TTL,
	})
	return err
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	id, _ := strconv.Atoi(recordID)
	_, err := c.client.Domains.DeleteRecord(ctx, zoneID, id)
	return err
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg providers.LBHealthCheckConfig) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) AddLBTarget(ctx context.Context, lbID, listenerID string, cfg providers.LBTargetConfig) (*providers.LBTarget, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ListLBTargets(ctx context.Context, lbID, listenerID string) ([]providers.LBTarget, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error {
	return fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ResizeLB(ctx context.Context, lbID, plan string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}

func (c *Client) GetLBMetrics(ctx context.Context, lbID string) (string, error) {
	return "", fmt.Errorf("digitalocean: method not available")
}

func (c *Client) ToggleLBProtection(ctx context.Context, lbID string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("digitalocean: method not available")
}
