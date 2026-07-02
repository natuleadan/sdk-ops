package cubepath

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/natuleadan/sdk-ops/providers"
)

type dnsRecordReq struct {
	Name    string `json:"name"`
	Type    string `json:"record_type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
}

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	resp, err := c.do(ctx, "GET", "/dns/zones", nil)
	if err != nil {
		return nil, fmt.Errorf("cubepath list dns zones: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var result []providers.DNSZone
	for _, r := range list {
		result = append(result, providers.DNSZone{
			ID:   val(r, "uuid"),
			Name: val(r, "domain"),
		})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	body := dnsRecordReq{
		Name:    r.Name,
		Type:    r.Type,
		Content: r.Value,
		TTL:     r.TTL,
	}
	_, err := c.do(ctx, "POST", fmt.Sprintf("/dns/zones/%s/records", zoneID), body)
	return err
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	_, err := c.do(ctx, "DELETE", fmt.Sprintf("/dns/zones/%s/records/%s", zoneID, recordID), nil)
	return err
}
