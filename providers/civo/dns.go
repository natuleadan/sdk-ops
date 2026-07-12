package civo

import (
	"context"
	"fmt"

	"github.com/civo/civogo"
	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	domains, err := c.client.ListDNSDomains()
	if err != nil {
		return nil, fmt.Errorf("civo: list dns zones: %w", err)
	}
	var result []providers.DNSZone
	for _, d := range domains {
		result = append(result, providers.DNSZone{
			ID:   d.ID,
			Name: d.Name,
		})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	cfg := &civogo.DNSRecordConfig{
		Name:  r.Name,
		Value: r.Value,
		TTL:   r.TTL,
	}
	switch r.Type {
	case "a":
		cfg.Type = civogo.DNSRecordTypeA
	case "cname":
		cfg.Type = civogo.DNSRecordTypeCName
	case "mx":
		cfg.Type = civogo.DNSRecordTypeMX
		cfg.Priority = r.Priority
	case "txt":
		cfg.Type = civogo.DNSRecordTypeTXT
	case "ns":
		cfg.Type = civogo.DNSRecordTypeNS
	case "srv":
		cfg.Type = civogo.DNSRecordTypeSRV
	default:
		cfg.Type = civogo.DNSRecordTypeA
	}
	_, err := c.client.CreateDNSRecord(zoneID, cfg)
	if err != nil {
		return fmt.Errorf("civo: create dns record: %w", err)
	}
	return nil
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	_, err := c.client.DeleteDNSRecord(&civogo.DNSRecord{ID: recordID})
	if err != nil {
		return fmt.Errorf("civo: delete dns record: %w", err)
	}
	return nil
}
