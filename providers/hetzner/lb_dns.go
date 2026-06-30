package hetzner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateBareMetal(ctx context.Context, cfg providers.BareMetalCreateConfig) (*providers.BareMetal, error) {
	return nil, fmt.Errorf("hetzner: bare metal not available — use a dedicated CX server type for dedicated VPS")
}

func (c *Client) DeleteBareMetal(ctx context.Context, id string) error {
	return fmt.Errorf("hetzner: bare metal not available")
}

func (c *Client) ListBareMetal(ctx context.Context) ([]providers.BareMetal, error) {
	return nil, fmt.Errorf("hetzner: bare metal not available")
}

func (c *Client) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	body := map[string]any{
		"name":              cfg.Name,
		"load_balancer_type": cfg.Plan,
		"location":          cfg.Location,
	}
	resp, err := c.raw().do("POST", "/load_balancers", body)
	if err != nil {
		return nil, fmt.Errorf("hetzner create lb: %w", err)
	}
	var r struct {
		LB map[string]any `json:"load_balancer"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.LoadBalancer{
		ID:   val(r.LB, "id"),
		Name: val(r.LB, "name"),
		IP:   val(r.LB, "public_net"),
	}, nil
}

func (c *Client) DeleteLB(ctx context.Context, id string) error {
	_, err := c.raw().do("DELETE", "/load_balancers/"+id, nil)
	return err
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	resp, err := c.raw().do("GET", "/load_balancers", nil)
	if err != nil {
		return nil, fmt.Errorf("hetzner list lb: %w", err)
	}
	var r struct {
		LBs []map[string]any `json:"load_balancers"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.LoadBalancer
	for _, lb := range r.LBs {
		result = append(result, providers.LoadBalancer{ID: val(lb, "id"), Name: val(lb, "name")})
	}
	return result, nil
}

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	// Hetzner DNS is a separate API: https://dns.hetzner.com/api/v1
	hc := &rawClient{
		token:   c.token,
		baseURL: "https://dns.hetzner.com/api/v1",
		http:    c.raw().http,
	}
	resp, err := hc.do("GET", "/zones", nil)
	if err != nil {
		return nil, fmt.Errorf("hetzner dns list zones: %w", err)
	}
	var r struct {
		Zones []map[string]any `json:"zones"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.DNSZone
	for _, z := range r.Zones {
		result = append(result, providers.DNSZone{
			ID:   val(z, "id"),
			Name: val(z, "name"),
		})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	hc := &rawClient{
		token:   c.token,
		baseURL: "https://dns.hetzner.com/api/v1",
		http:    c.raw().http,
	}
	body := map[string]any{
		"zone_id": zoneID,
		"type":    r.Type,
		"name":    r.Name,
		"value":   r.Value,
		"ttl":     r.TTL,
	}
	_, err := hc.do("POST", "/records", body)
	return err
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	hc := &rawClient{
		token:   c.token,
		baseURL: "https://dns.hetzner.com/api/v1",
		http:    c.raw().http,
	}
	_, err := hc.do("DELETE", "/records/"+recordID, nil)
	return err
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("hetzner: method not available")
}

func (c *Client) UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("hetzner: method not available")
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	return fmt.Errorf("hetzner: method not available")
}

func (c *Client) SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg providers.LBHealthCheckConfig) error {
	return fmt.Errorf("hetzner: method not available")
}

func (c *Client) AddLBTarget(ctx context.Context, lbID, listenerID string, cfg providers.LBTargetConfig) (*providers.LBTarget, error) {
	return nil, fmt.Errorf("hetzner: method not available")
}

func (c *Client) ListLBTargets(ctx context.Context, lbID, listenerID string) ([]providers.LBTarget, error) {
	return nil, fmt.Errorf("hetzner: method not available")
}

func (c *Client) DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error {
	return fmt.Errorf("hetzner: method not available")
}

func (c *Client) ResizeLB(ctx context.Context, lbID, plan string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("hetzner: method not available")
}

func (c *Client) GetLBMetrics(ctx context.Context, lbID string) (string, error) {
	return "", fmt.Errorf("hetzner: method not available")
}

func (c *Client) ToggleLBProtection(ctx context.Context, lbID string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("hetzner: method not available")
}
