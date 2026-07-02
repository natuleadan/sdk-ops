package cubepath

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateBareMetal(ctx context.Context, cfg providers.BareMetalCreateConfig) (*providers.BareMetal, error) {
	label := cfg.Label
	if label == "" {
		label = cfg.Hostname
	}
	if label == "" {
		label = "sdk-ops-bm-" + time.Now().Format("20060102-150405")
	}
	name := cfg.Hostname
	if name == "" {
		name = label
	}

	body := map[string]any{
		"model_name":    cfg.Plan,
		"location_name": cfg.Location,
		"hostname":      name,
		"label":         label,
		"os_name":       cfg.Template,
		"password":      cfg.Password,
	}
	if len(cfg.SSHKeyIDs) > 0 {
		body["ssh_key_ids"] = cfg.SSHKeyIDs
	}

	path := fmt.Sprintf("/baremetal/deploy/%d", c.projectID)
	respBody, err := c.do(ctx, "POST", path, body)
	if err != nil {
		return nil, fmt.Errorf("cubepath deploy baremetal: %w", err)
	}

	var result struct {
		ID       int    `json:"id"`
		Hostname string `json:"hostname"`
		Label    string `json:"label"`
		Status   string `json:"status"`
		Model    string `json:"model_name"`
		Location string `json:"location_name"`
		IP       string `json:"ip_address"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse baremetal response: %w\nbody: %s", err, string(respBody))
	}

	return &providers.BareMetal{
		ID:       fmt.Sprintf("%d", result.ID),
		Name:     result.Hostname,
		Label:    result.Label,
		Status:   result.Status,
		Plan:     result.Model,
		Location: result.Location,
		IP:       result.IP,
	}, nil
}

func (c *Client) DeleteBareMetal(ctx context.Context, id string) error {
	return fmt.Errorf("cubepath: bare metal cannot be destroyed via API (physical hardware). Contact support")
}

func (c *Client) ListBareMetal(ctx context.Context) ([]providers.BareMetal, error) {
	respBody, err := c.do(ctx, "GET", "/baremetal/", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list baremetal: %w", err)
	}

	var list []struct {
		ID       int    `json:"id"`
		Hostname string `json:"hostname"`
		Label    string `json:"label"`
		Status   string `json:"status"`
		Model    string `json:"model_name"`
		Location string `json:"location_name"`
		IP       string `json:"ip_address"`
	}
	if err := json.Unmarshal(respBody, &list); err != nil {
		return nil, fmt.Errorf("parse baremetal list: %w\nbody: %s", err, string(respBody))
	}

	var result []providers.BareMetal
	for _, bm := range list {
		result = append(result, providers.BareMetal{
			ID:       fmt.Sprintf("%d", bm.ID),
			Name:     bm.Hostname,
			Label:    bm.Label,
			Status:   bm.Status,
			Plan:     bm.Model,
			Location: bm.Location,
			IP:       bm.IP,
		})
	}
	return result, nil
}
