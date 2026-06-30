package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateBareMetal(ctx context.Context, cfg providers.BareMetalCreateConfig) (*providers.BareMetal, error) {
	label := cfg.Label
	if label == "" {
		label = cfg.Hostname
	}

	bm, _, err := c.client.BareMetalServer.Create(ctx, &govultr.BareMetalCreate{
		Region:   cfg.Location,
		Plan:     cfg.Plan,
		Label:    label,
		Hostname: cfg.Hostname,
		ImageID:  cfg.Template,
		UserData: cfg.UserData,
	})
	if err != nil {
		return nil, fmt.Errorf("vultr create baremetal: %w", err)
	}
	return &providers.BareMetal{
		ID:       bm.ID,
		Name:     bm.Label,
		Label:    cfg.Label,
		Status:   bm.Status,
		Plan:     bm.Plan,
		Location: bm.Region,
		IP:       bm.MainIP,
	}, nil
}

func (c *Client) DeleteBareMetal(ctx context.Context, id string) error {
	err := c.client.BareMetalServer.Delete(ctx, id)
	return err
}

func (c *Client) ListBareMetal(ctx context.Context) ([]providers.BareMetal, error) {
	bms, _, _, err := c.client.BareMetalServer.List(ctx, &govultr.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("vultr list baremetal: %w", err)
	}
	var result []providers.BareMetal
	for _, bm := range bms {
		result = append(result, providers.BareMetal{
			ID: bm.ID, Name: bm.Label, Status: bm.Status,
			Plan: bm.Plan, Location: bm.Region, IP: bm.MainIP,
		})
	}
	return result, nil
}

func (c *Client) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	lb, _, err := c.client.LoadBalancer.Create(ctx, &govultr.LoadBalancerReq{
		Label:              cfg.Name,
		Region:             cfg.Location,
		BalancingAlgorithm: cfg.Algorithm,
	})
	if err != nil {
		return nil, fmt.Errorf("vultr create lb: %w", err)
	}
	return &providers.LoadBalancer{
		ID:   lb.ID,
		Name: lb.Label,
		IP:   lb.IPV4,
	}, nil
}

func (c *Client) DeleteLB(ctx context.Context, id string) error {
	err := c.client.LoadBalancer.Delete(ctx, id)
	return err
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	lbs, _, _, err := c.client.LoadBalancer.List(ctx, &govultr.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("vultr list lb: %w", err)
	}
	var result []providers.LoadBalancer
	for _, lb := range lbs {
		result = append(result, providers.LoadBalancer{ID: lb.ID, Name: lb.Label, IP: lb.IPV4})
	}
	return result, nil
}

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	domains, _, _, err := c.client.Domain.List(ctx, &govultr.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("vultr list domains: %w", err)
	}
	var result []providers.DNSZone
	for _, d := range domains {
		result = append(result, providers.DNSZone{
			ID:   d.Domain,
			Name: d.Domain,
		})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	req := &govultr.DomainRecordCreateReq{
		Type: r.Type,
		Name: r.Name,
		Data: r.Value,
		TTL:  r.TTL,
	}
	_, _, err := c.client.DomainRecord.Create(ctx, zoneID, req)
	return err
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	err := c.client.DomainRecord.Delete(ctx, zoneID, recordID)
	return err
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	return fmt.Errorf("vultr: method not available")
}

func (c *Client) SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg providers.LBHealthCheckConfig) error {
	return fmt.Errorf("vultr: method not available")
}

func (c *Client) AddLBTarget(ctx context.Context, lbID, listenerID string, cfg providers.LBTargetConfig) (*providers.LBTarget, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) ListLBTargets(ctx context.Context, lbID, listenerID string) ([]providers.LBTarget, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error {
	return fmt.Errorf("vultr: method not available")
}

func (c *Client) ResizeLB(ctx context.Context, lbID, plan string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("vultr: method not available")
}

func (c *Client) GetLBMetrics(ctx context.Context, lbID string) (string, error) {
	return "", fmt.Errorf("vultr: method not available")
}

func (c *Client) ToggleLBProtection(ctx context.Context, lbID string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("vultr: method not available")
}
