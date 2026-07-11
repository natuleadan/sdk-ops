package vultr

import (
	"context"
	"fmt"

	"github.com/vultr/govultr/v3"
)

type FirewallGroup struct {
	ID        string `json:"id"`
	Description string `json:"description"`
	DateCreated string `json:"date_created"`
	DateModified string `json:"date_modified"`
	InstanceCount int    `json:"instance_count"`
	RuleCount     int    `json:"rule_count"`
	MaxRuleCount  int    `json:"max_rule_count"`
}

type FirewallRule struct {
	ID         int    `json:"id"`
	Action     string `json:"action"`
	IPType     string `json:"ip_type"`
	Protocol   string `json:"protocol"`
	Port       string `json:"port"`
	Subnet     string `json:"subnet"`
	SubnetSize int    `json:"subnet_size"`
	Source     string `json:"source"`
	Notes      string `json:"notes"`
}

func (c *Client) ListFirewallGroups(ctx context.Context) ([]FirewallGroup, error) {
	groups, _, resp, err := c.client.FirewallGroup.List(ctx, &govultr.ListOptions{})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list firewall groups: %w", err)
	}
	var result []FirewallGroup
	for _, g := range groups {
		result = append(result, FirewallGroup{
			ID: g.ID, Description: g.Description,
			DateCreated: g.DateCreated, DateModified: g.DateModified,
			InstanceCount: g.InstanceCount, RuleCount: g.RuleCount, MaxRuleCount: g.MaxRuleCount,
		})
	}
	return result, nil
}

func (c *Client) CreateFirewallGroup(ctx context.Context, desc string) (*FirewallGroup, error) {
	group, resp, err := c.client.FirewallGroup.Create(ctx, &govultr.FirewallGroupReq{Description: desc})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr create firewall group: %w", err)
	}
	return &FirewallGroup{ID: group.ID, Description: group.Description}, nil
}

func (c *Client) DeleteFirewallGroup(ctx context.Context, id string) error {
	err := c.client.FirewallGroup.Delete(ctx, id)
	if err != nil {
		return fmt.Errorf("vultr delete firewall group: %w", err)
	}
	return nil
}

func (c *Client) ListFirewallRules(ctx context.Context, groupID string) ([]FirewallRule, error) {
	rules, _, resp, err := c.client.FirewallRule.List(ctx, groupID, &govultr.ListOptions{})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr list firewall rules: %w", err)
	}
	var result []FirewallRule
	for _, r := range rules {
		result = append(result, FirewallRule{
			ID: r.ID, Action: r.Action, IPType: r.IPType, Protocol: r.Protocol,
			Subnet: r.Subnet, SubnetSize: r.SubnetSize, Port: r.Port, Source: r.Source, Notes: r.Notes,
		})
	}
	return result, nil
}

func (c *Client) CreateFirewallRule(ctx context.Context, groupID, ipType, protocol, subnet, port, notes, source string, subnetSize int) (*FirewallRule, error) {
	rule, resp, err := c.client.FirewallRule.Create(ctx, groupID, &govultr.FirewallRuleReq{
		IPType: ipType, Protocol: protocol, Subnet: subnet, SubnetSize: subnetSize, Port: port, Notes: notes, Source: source,
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("vultr create firewall rule: %w", err)
	}
	return &FirewallRule{
		ID: rule.ID, Action: rule.Action, IPType: rule.IPType, Protocol: rule.Protocol,
		Subnet: rule.Subnet, SubnetSize: rule.SubnetSize, Port: rule.Port, Source: rule.Source, Notes: rule.Notes,
	}, nil
}

func (c *Client) DeleteFirewallRule(ctx context.Context, groupID string, ruleID int) error {
	return c.client.FirewallRule.Delete(ctx, groupID, ruleID)
}
