package cubepath

import (
	"context"
	"encoding/json"
	"fmt"

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
	resp, err := c.do("POST", "/loadbalancer/", body)
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
	_, err := c.do("DELETE", "/loadbalancer/"+id, nil)
	return err
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	resp, err := c.do("GET", "/loadbalancer/", nil)
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
