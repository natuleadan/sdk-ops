package civo

import (
	"context"
	"fmt"
	"strconv"

	"github.com/civo/civogo"
	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	lb, err := c.client.CreateLoadBalancer(&civogo.LoadBalancerConfig{
		Name:     cfg.Name,
		Region:   regionAlias(cfg.Location),
		Algorithm: cfg.Algorithm,
		Backends: []civogo.LoadBalancerBackendConfig{
			{IP: "0.0.0.0", Protocol: "TCP", SourcePort: 80, TargetPort: 8080},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("civo: create lb: %w", err)
	}
	status := "active"
	if lb.State != "" {
		status = lb.State
	}
	return &providers.LoadBalancer{
		ID:       lb.ID,
		Name:     lb.Name,
		Status:   status,
		IP:       lb.PublicIP,
		Location: cfg.Location,
	}, nil
}

var knownRegions = []string{"NYC1", "LON1", "FRA1", "MUM1", "PHX1"}

func (c *Client) tryRegions(fn func(*civogo.Client) error) error {
	var lastErr error
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil { lastErr = err; continue }
		if err := fn(cc); err != nil { lastErr = err; continue }
		return nil
	}
	return lastErr
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	var all []providers.LoadBalancer
	for _, region := range knownRegions {
		cc, err := c.withRegion(region)
		if err != nil { continue }
		lbs, err := cc.ListLoadBalancers()
		if err != nil { continue }
		for _, lb := range lbs {
			all = append(all, providers.LoadBalancer{
				ID: lb.ID, Name: lb.Name, Status: lb.State, IP: lb.PublicIP,
			})
		}
	}
	return all, nil
}

func (c *Client) DeleteLB(ctx context.Context, id string) error {
	return c.tryRegions(func(cc *civogo.Client) error {
		_, err := cc.DeleteLoadBalancer(id)
		return err
	})
}

func (c *Client) GetLB(ctx context.Context, id string) (*providers.LoadBalancer, error) {
	var result *providers.LoadBalancer
	err := c.tryRegions(func(cc *civogo.Client) error {
		lb, err := cc.GetLoadBalancer(id)
		if err != nil { return err }
		result = &providers.LoadBalancer{
			ID: lb.ID, Name: lb.Name, Status: lb.State, IP: lb.PublicIP,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func toPort(n int) int32 {
	if n < 0 || n > 65535 {
		return 0
	}
	return int32(n)
}

func backendToConfig(b civogo.LoadBalancerBackend) civogo.LoadBalancerBackendConfig {
	return civogo.LoadBalancerBackendConfig(b)
}

func listenerID(lbID string, port int32) string {
	return lbID + ":" + strconv.Itoa(int(port))
}

func parsePort(id string) int32 {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == ':' {
			if p, err := strconv.Atoi(id[i+1:]); err == nil {
				return toPort(p)
			}
			break
		}
	}
	return 0
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	lb, err := c.client.GetLoadBalancer(lbID)
	if err != nil {
		return nil, fmt.Errorf("civo: get lb: %w", err)
	}
	prot := "TCP"
	if cfg.Protocol != "" {
		prot = cfg.Protocol
	}
	backend := civogo.LoadBalancerBackendConfig{
		Protocol:   prot,
		SourcePort: toPort(cfg.Port),
		TargetPort: toPort(cfg.TargetPort),
	}
	var backends []civogo.LoadBalancerBackendConfig
	for _, b := range lb.Backends {
		backends = append(backends, backendToConfig(b))
	}
	backends = append(backends, backend)
	if err := c.updateLBBackends(ctx, lbID, backends); err != nil {
		return nil, err
	}
	return &providers.LBListener{
		ID:         listenerID(lbID, toPort(cfg.Port)),
		Port:       cfg.Port,
		TargetPort: cfg.TargetPort,
		Protocol:   prot,
	}, nil
}

func (c *Client) updateLBBackends(_ context.Context, lbID string, backends []civogo.LoadBalancerBackendConfig) error {
	return c.tryRegions(func(cc *civogo.Client) error {
		_, err := cc.UpdateLoadBalancer(lbID, &civogo.LoadBalancerUpdateConfig{
			Region:   "NYC1",
			Backends: backends,
		})
		return err
	})
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	lb, err := c.client.GetLoadBalancer(lbID)
	if err != nil {
		return fmt.Errorf("civo: get lb: %w", err)
	}
	port := parsePort(listenerID)
	var backends []civogo.LoadBalancerBackendConfig
	for _, b := range lb.Backends {
		if b.SourcePort != port {
			backends = append(backends, backendToConfig(b))
		}
	}
	return c.updateLBBackends(ctx, lbID, backends)
}

func (c *Client) UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	lb, err := c.client.GetLoadBalancer(lbID)
	if err != nil {
		return nil, fmt.Errorf("civo: get lb: %w", err)
	}
	prot := "tcp"
	if cfg.Protocol != "" {
		prot = cfg.Protocol
	}
	port := parsePort(listenerID)
	var backends []civogo.LoadBalancerBackendConfig
	for _, b := range lb.Backends {
		bcfg := backendToConfig(b)
		if b.SourcePort == port {
			bcfg.Protocol = prot
			bcfg.SourcePort = toPort(cfg.Port)
			bcfg.TargetPort = toPort(cfg.TargetPort)
		}
		backends = append(backends, bcfg)
	}
	if err := c.updateLBBackends(ctx, lbID, backends); err != nil {
		return nil, err
	}
	return &providers.LBListener{
		ID:         listenerID,
		Port:       cfg.Port,
		TargetPort: cfg.TargetPort,
		Protocol:   prot,
	}, nil
}

func (c *Client) SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg providers.LBHealthCheckConfig) error {
	return fmt.Errorf("civo: method not available")
}

func (c *Client) AddLBTarget(ctx context.Context, lbID, listenerID string, cfg providers.LBTargetConfig) (*providers.LBTarget, error) {
	lb, err := c.client.GetLoadBalancer(lbID)
	if err != nil {
		return nil, fmt.Errorf("civo: get lb: %w", err)
	}
	prot := "tcp"
	sp := parsePort(listenerID)
	backend := civogo.LoadBalancerBackendConfig{
		IP:         cfg.TargetID,
		Protocol:   prot,
		SourcePort: sp,
		TargetPort: toPort(cfg.Port),
	}
	var backends []civogo.LoadBalancerBackendConfig
	for _, b := range lb.Backends {
		backends = append(backends, backendToConfig(b))
	}
	backends = append(backends, backend)
	if err := c.updateLBBackends(ctx, lbID, backends); err != nil {
		return nil, err
	}
	return &providers.LBTarget{
		ID:       fmt.Sprintf("%s-%s", lbID, cfg.TargetID),
		Type:     cfg.Type,
		TargetID: cfg.TargetID,
		Port:     cfg.Port,
		Weight:   cfg.Weight,
	}, nil
}

func (c *Client) ListLBTargets(ctx context.Context, lbID, listenerID string) ([]providers.LBTarget, error) {
	lb, err := c.client.GetLoadBalancer(lbID)
	if err != nil {
		return nil, fmt.Errorf("civo: get lb: %w", err)
	}
	port := parsePort(listenerID)
	var result []providers.LBTarget
	for _, b := range lb.Backends {
		if port != 0 && b.SourcePort != port {
			continue
		}
		result = append(result, providers.LBTarget{
			ID:       fmt.Sprintf("%s-%s", lbID, b.IP),
			TargetID: b.IP,
			Port:     int(b.TargetPort),
		})
	}
	return result, nil
}

func (c *Client) DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error {
	lb, err := c.client.GetLoadBalancer(lbID)
	if err != nil {
		return fmt.Errorf("civo: get lb: %w", err)
	}
	port := parsePort(listenerID)
	var backends []civogo.LoadBalancerBackendConfig
	for _, b := range lb.Backends {
		id := fmt.Sprintf("%s-%s", lbID, b.IP)
		if id == targetID || (port != 0 && b.SourcePort == port) {
			continue
		}
		backends = append(backends, backendToConfig(b))
	}
	return c.updateLBBackends(ctx, lbID, backends)
}

func (c *Client) ResizeLB(ctx context.Context, lbID, plan string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("civo: method not available")
}

func (c *Client) GetLBMetrics(ctx context.Context, lbID string) (string, error) {
	return "", fmt.Errorf("civo: method not available")
}

func (c *Client) ToggleLBProtection(ctx context.Context, lbID string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("civo: method not available")
}
