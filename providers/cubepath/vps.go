package cubepath

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	var sshKeys []int
	for _, id := range cfg.SSHKeyIDs {
		if n, err := strconv.Atoi(id); err == nil {
			sshKeys = append(sshKeys, n)
		}
	}

	label := cfg.Label
	if label == "" {
		label = cfg.Hostname
	}
	if label == "" {
		label = "sdk-ops-" + time.Now().Format("20060102-150405")
	}

	name := cfg.Hostname
	if name == "" {
		name = "vps-" + time.Now().Format("20060102-150405")
	}

	body := map[string]any{
		"label":          label,
		"template_name":  cfg.Template,
		"plan_name":      cfg.Plan,
		"location_name":  cfg.Location,
		"name":           name,
		"ssh_key_ids":    sshKeys,
		"ipv4":           cfg.EnableIPv4,
		"ipv6":           cfg.EnableIPv6,
		"enable_backups": cfg.Backups,
		"user":           cfg.User,
		"password":       cfg.Password,
		"user_data":      cfg.UserData,
	}

	path := fmt.Sprintf("/vps/create/%d", c.projectID)
	respBody, err := c.do(ctx, "POST", path, body)
	if err != nil {
		return nil, fmt.Errorf("create vps: %w", err)
	}

	var result struct {
		VPSID  int    `json:"vps_id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Plan   string `json:"plan"`
		Loc    string `json:"location"`
		IP     string `json:"ipv4_address"`
		IPv6   string `json:"ipv6_address"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w\nbody: %s", err, string(respBody))
	}

	vps := &providers.VPS{
		ID:       fmt.Sprintf("%d", result.VPSID),
		Name:     result.Name,
		Label:    result.Name,
		Status:   result.Status,
		Plan:     result.Plan,
		Location: result.Loc,
		IP:       result.IP,
		IPv6:     result.IPv6,
	}

	// Wait for VPS to become active
	fmt.Printf("  Waiting for VPS %s to become active (current: %s)...\n", vps.ID, vps.Status)
	for i := range 30 {
		time.Sleep(5 * time.Second)
		current, err := c.GetVPS(ctx, vps.ID)
		if err == nil && current.Status == "active" {
			vps.IP = current.IP
			vps.Status = current.Status
			fmt.Printf("  ✅ VPS %s is active @ %s\n", vps.ID, vps.IP)
			break
		}
		if i%6 == 0 {
			fmt.Printf("  Still waiting... (%ds)\n", (i+1)*5)
		}
	}

	return vps, nil
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	_, err := c.do(ctx, "POST", "/vps/destroy/"+id, nil)
	if err != nil {
		return fmt.Errorf("delete vps: %w", err)
	}
	return nil
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	respBody, err := c.do(ctx, "GET", "/projects/", nil)
	if err != nil {
		return nil, fmt.Errorf("list vps: %w", err)
	}

	var projects []struct {
		VPS []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Label    string `json:"label"`
			Hostname string `json:"hostname"`
			Status   string `json:"status"`
			Plan     struct {
				PlanName string `json:"plan_name"`
			} `json:"plan"`
			Template struct {
				TemplateName string `json:"template_name"`
			} `json:"template"`
			Location struct {
				LocationName string `json:"location_name"`
			} `json:"location"`
			FloatingIPs struct {
				List []struct {
					Address string `json:"address"`
				} `json:"list"`
			} `json:"floating_ips"`
		} `json:"vps"`
	}

	if err := json.Unmarshal(respBody, &projects); err != nil {
		return nil, fmt.Errorf("parse list: %w", err)
	}

	var result []providers.VPS
	for _, project := range projects {
		for _, v := range project.VPS {
			vps := providers.VPS{
				ID:       fmt.Sprintf("%d", v.ID),
				Name:     v.Name,
				Label:    v.Label,
				Status:   v.Status,
				Plan:     v.Plan.PlanName,
				Location: v.Location.LocationName,
				Template: v.Template.TemplateName,
			}
			for _, ip := range v.FloatingIPs.List {
				if ip.Address != "" {
					vps.IP = ip.Address
					break
				}
			}
			result = append(result, vps)
		}
	}

	return result, nil
}

func (c *Client) GetVPS(ctx context.Context, id string) (*providers.VPS, error) {
	list, err := c.ListVPS(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v.ID == id {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("vps %s not found", id)
}
