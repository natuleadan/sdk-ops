package cubepath

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/natuleadan/sdk-ops/providers"
)

type lbCreateRequest struct {
	Name      string `json:"name,omitempty"`
	Label     string `json:"label,omitempty"`
	Plan      string `json:"plan_name,omitempty"`
	Location  string `json:"location_name,omitempty"`
	ProjectID int    `json:"project_id"`
}

func (c *Client) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	body := lbCreateRequest{
		Name:      cfg.Name,
		Label:     cfg.Label,
		Plan:      cfg.Plan,
		Location:  cfg.Location,
		ProjectID: c.projectID,
	}
	resp, err := c.do(ctx, "POST", "/loadbalancer/", body)
	if err != nil {
		return nil, fmt.Errorf("cubepath create lb: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.LoadBalancer{
		ID:   val(r, "uuid"),
		Name: val(r, "name"),
		IP:   val(r, "ipv4_address"),
	}, nil
}

func (c *Client) DeleteLB(ctx context.Context, id string) error {
	_, err := c.do(ctx, "DELETE", "/loadbalancer/"+id, nil)
	return err
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	resp, err := c.do(ctx, "GET", "/loadbalancer/", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list lb: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.LoadBalancer
	for _, r := range list {
		result = append(result, providers.LoadBalancer{
			ID:   val(r, "uuid"),
			Name: val(r, "name"),
			IP:   val(r, "ipv4_address"),
		})
	}
	return result, nil
}

type lbListenerRequest struct {
	Port       int    `json:"port"`
	TargetPort int    `json:"target_port"`
	Protocol   string `json:"protocol"`
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	body := lbListenerRequest{Port: cfg.Port, TargetPort: cfg.TargetPort, Protocol: cfg.Protocol}
	resp, err := c.do(ctx, "POST", "/loadbalancer/"+lbID+"/listeners", body)
	if err != nil {
		return nil, fmt.Errorf("cubepath create listener: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.LBListener{
		ID: val(r, "uuid"), Port: cfg.Port, TargetPort: cfg.TargetPort, Protocol: cfg.Protocol,
	}, nil
}

func (c *Client) UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	body := lbListenerRequest{Port: cfg.Port, TargetPort: cfg.TargetPort, Protocol: cfg.Protocol}
	resp, err := c.do(ctx, "PATCH", "/loadbalancer/"+lbID+"/listeners/"+listenerID, body)
	if err != nil {
		return nil, fmt.Errorf("cubepath update listener: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.LBListener{ID: val(r, "uuid")}, nil
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	_, err := c.do(ctx, "DELETE", "/loadbalancer/"+lbID+"/listeners/"+listenerID, nil)
	return err
}

func (c *Client) SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg providers.LBHealthCheckConfig) error {
	_, err := c.do(ctx, "PUT", fmt.Sprintf("/loadbalancer/%s/listeners/%s/health-check", lbID, listenerID), cfg)
	return err
}

type lbTargetRequest struct {
	Type     string `json:"type"`
	TargetID string `json:"target_uuid"`
	Port     int    `json:"port"`
	Weight   int    `json:"weight,omitempty"`
}

func (c *Client) AddLBTarget(ctx context.Context, lbID, listenerID string, cfg providers.LBTargetConfig) (*providers.LBTarget, error) {
	body := lbTargetRequest{Type: cfg.Type, TargetID: cfg.TargetID, Port: cfg.Port, Weight: cfg.Weight}
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/loadbalancer/%s/listeners/%s/targets", lbID, listenerID), body)
	if err != nil {
		return nil, fmt.Errorf("cubepath add target: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.LBTarget{ID: val(r, "uuid"), Type: cfg.Type, TargetID: cfg.TargetID, Port: cfg.Port}, nil
}

func (c *Client) ListLBTargets(ctx context.Context, lbID, listenerID string) ([]providers.LBTarget, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/loadbalancer/%s/listeners/%s/targets", lbID, listenerID), nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list targets: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.LBTarget
	for _, r := range list {
		result = append(result, providers.LBTarget{
			ID: val(r, "uuid"), Type: val(r, "type"),
			TargetID: val(r, "target_uuid"), Port: cfgPort(r),
			Status: val(r, "status"),
		})
	}
	return result, nil
}

func (c *Client) DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error {
	_, err := c.do(ctx, "POST", fmt.Sprintf("/loadbalancer/%s/listeners/%s/targets/%s/drain", lbID, listenerID, targetID), nil)
	return err
}

func (c *Client) ResizeLB(ctx context.Context, lbID, plan string) (*providers.LoadBalancer, error) {
	resp, err := c.do(ctx, "POST", "/loadbalancer/"+lbID+"/resize", map[string]string{"plan": plan})
	if err != nil {
		return nil, fmt.Errorf("cubepath resize lb: %w", err)
	}
	var r map[string]any
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &providers.LoadBalancer{ID: val(r, "uuid"), Plan: val(r, "plan")}, nil
}

func (c *Client) GetLBMetrics(ctx context.Context, lbID string) (string, error) {
	resp, err := c.do(ctx, "GET", "/loadbalancer/"+lbID+"/metrics", nil)
	if err != nil {
		return "", fmt.Errorf("cubepath lb metrics: %w", err)
	}
	return string(resp), nil
}

func (c *Client) ToggleLBProtection(ctx context.Context, lbID string) (*providers.LoadBalancer, error) {
	_, err := c.do(ctx, "POST", "/loadbalancer/"+lbID+"/protection", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath toggle lb protection: %w", err)
	}
	return &providers.LoadBalancer{ID: lbID}, nil
}

func cfgPort(r map[string]any) int {
	var port int
	if _, err := fmt.Sscanf(val(r, "port"), "%d", &port); err != nil { log.Printf("cubepath: parse port error: %v", err) }
	return port
}
