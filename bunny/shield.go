package bunny

import (
	"context"
	"fmt"
)

type ShieldZone struct {
	ID             int64  `json:"Id,omitempty"`
	PullZoneID     int64  `json:"PullZoneId,omitempty"`
	Name           string `json:"Name,omitempty"`
	Status         string `json:"Status,omitempty"`
}

type WAFRule struct {
	ID        int64  `json:"Id,omitempty"`
	RuleID    string `json:"RuleId,omitempty"`
	Action    string `json:"Action,omitempty"`
	Enabled   bool   `json:"Enabled,omitempty"`
}

type RateLimitRule struct {
	ID            int64  `json:"Id,omitempty"`
	RequestsLimit int32  `json:"RequestsLimit,omitempty"`
	WindowLength  int32  `json:"WindowLengthInSeconds,omitempty"`
	Action        string `json:"Action,omitempty"`
	Path          string `json:"Path,omitempty"`
}

type BotDetectionConfig struct {
	Enabled       bool   `json:"Enabled,omitempty"`
	Action        string `json:"Action,omitempty"`
}

type CustomWAFRule struct {
	ID          int64  `json:"Id,omitempty"`
	Name        string `json:"Name,omitempty"`
	Description string `json:"Description,omitempty"`
	Condition   string `json:"Condition,omitempty"`
	Action      string `json:"Action,omitempty"`
	Enabled     bool   `json:"Enabled,omitempty"`
}

// Shield Zone CRUD

func (c *Client) ListShieldZones(ctx context.Context) ([]ShieldZone, error) {
	var resp struct {
		Data []ShieldZone `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/shield-zones", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) CreateShieldZone(ctx context.Context, pullZoneID int64) (*ShieldZone, error) {
	var zone ShieldZone
	err := c.Post(ctx, APIShield, "/shield-zone", map[string]int64{"pullZoneId": pullZoneID}, &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

func (c *Client) GetShieldZone(ctx context.Context, id int64) (*ShieldZone, error) {
	var zone ShieldZone
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d", id), &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

func (c *Client) DeleteShieldZone(ctx context.Context, id int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/shield-zone/%d", id), nil)
}

// WAF Rules

func (c *Client) ListAvailableWAFFules(ctx context.Context) ([]WAFRule, error) {
	var resp struct {
		Rules []WAFRule `json:"Rules,omitempty"`
	}
	err := c.Get(ctx, APIShield, "/waf/rules", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

func (c *Client) ListCustomWAFRules(ctx context.Context, zoneID int64) ([]CustomWAFRule, error) {
	var resp struct {
		Rules []CustomWAFRule `json:"Rules,omitempty"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/waf/custom-rules/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

func (c *Client) CreateCustomWAFRule(ctx context.Context, rule CustomWAFRule) (*CustomWAFRule, error) {
	var resp CustomWAFRule
	err := c.Post(ctx, APIShield, "/waf/custom-rule", rule, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteCustomWAFRule(ctx context.Context, ruleID int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/waf/custom-rule/%d", ruleID), nil)
}

// Rate Limits

func (c *Client) ListRateLimits(ctx context.Context, zoneID int64) ([]RateLimitRule, error) {
	var resp struct {
		RateLimits []RateLimitRule `json:"RateLimits,omitempty"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/rate-limits/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.RateLimits, nil
}

func (c *Client) CreateRateLimit(ctx context.Context, rule RateLimitRule) (*RateLimitRule, error) {
	var resp RateLimitRule
	err := c.Post(ctx, APIShield, "/rate-limit", rule, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteRateLimit(ctx context.Context, ruleID int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/rate-limit/%d", ruleID), nil)
}

// Bot Detection

func (c *Client) GetBotDetection(ctx context.Context, zoneID int64) (*BotDetectionConfig, error) {
	var cfg BotDetectionConfig
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-detection", zoneID), &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Client) UpdateBotDetection(ctx context.Context, zoneID int64, cfg BotDetectionConfig) error {
	return c.Put(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-detection", zoneID), cfg, nil)
}
